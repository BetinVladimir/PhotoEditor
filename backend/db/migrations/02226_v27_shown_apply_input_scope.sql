-- +goose Up
-- Registration scope only; caller has validated complete locked decision/receipt
-- and reserved quota in the same transaction. No worker or mutation grant.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_apply_input_scope(p_tenant uuid,p_project uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_review_batches b ON b.tenant_id=r.tenant_id AND b.child_run_id=r.id AND b.action='apply'
 JOIN photo.v2_workflows w ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id
 JOIN photo.semantic_targets target ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=r.id
 JOIN photo.projects project ON project.tenant_id=b.tenant_id AND project.id=b.project_id AND project.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=w.tenant_id AND d.run_id=w.run_id AND d.owner_id=t.id
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=params.id
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND b.project_id=p_project AND w.project_id=p_project
 AND target.project_id=p_project AND target.state='queued' AND b.actor_id=r.created_by
 AND r.kind='apply' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot->>'project_id'=p_project::text
 AND jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes')='array'
 AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested AND t.inputs='[]'::jsonb
 AND NOT(t.execution_profile ? 'v2_input_manifest')
 AND t.operation='semantic.adopt_shown_exposure' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_exposure:'||target.id::text)
 AND t.node_key='semantic.adopt_shown_exposure:'||target.id::text
 AND t.params_artifact_id=photo.semantic_uuid_v5(t.id,'Params')
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node
 WHERE node->>'task_id'=t.id::text AND node->>'target_id'=target.id::text
 AND node->>'params_artifact_id'=params.id::text AND node->>'params_sha'=params.sha256)
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL
 AND params.object_version NOT IN('','null') AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 AND d.sha256=params.sha256 AND d.bytes=params.bytes AND d.bucket=params.bucket AND d.object_version=params.object_version
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
 AND q.share_id IS NULL AND q.files=0 AND q.bytes>0 AND q.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_shown_apply_input_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_apply_input_scope(uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE OR REPLACE FUNCTION photo.guard_v2_artifact_link() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE artifact photo.artifacts%ROWTYPE; parent_ok boolean;
BEGIN
 SELECT * INTO artifact FROM photo.artifacts a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.artifact_id;
 IF artifact.id IS NULL OR artifact.sha256<>NEW.sha256 OR artifact.state<>'ready'
  OR artifact.object_version IS NULL OR artifact.verified_at IS NULL
  OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'V2_ARTIFACT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 CASE TG_TABLE_NAME
  WHEN 'v2_documents' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_document_streams s
    WHERE s.tenant_id=NEW.tenant_id AND s.project_id=NEW.project_id AND s.id=NEW.stream_id)
    INTO parent_ok;
  WHEN 'v2_document_refs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_documents d
    WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id AND d.id=NEW.document_id)
    INTO parent_ok;
  WHEN 'v2_artifact_links' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_workflows w
    WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id AND w.id=NEW.workflow_id)
    INTO parent_ok;
  WHEN 'v2_task_inputs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.tasks t
    JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
    WHERE t.tenant_id=NEW.tenant_id AND t.id=NEW.task_id AND w.project_id=NEW.project_id)
    OR photo.semantic_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_preapply_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_shown_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    INTO parent_ok;
  ELSE
   RAISE EXCEPTION 'V2_ARTIFACT_LINK_TABLE_INVALID' USING ERRCODE='23514';
 END CASE;
 IF parent_ok IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'V2_ARTIFACT_LINK_SCOPE_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
