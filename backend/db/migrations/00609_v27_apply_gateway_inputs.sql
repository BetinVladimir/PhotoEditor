-- +goose Up
-- Keep existing V2/cache authorization; add only activated approved Apply inputs.
-- +goose StatementBegin
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) RENAME TO gateway_input_before_semantic_apply;
REVOKE ALL ON FUNCTION photo.gateway_input_before_semantic_apply(uuid,integer,bigint,uuid,text,uuid) FROM photo_gateway;
CREATE FUNCTION photo.gateway_input(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_artifact uuid)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE permitted record;
BEGIN
 SELECT * INTO permitted FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate);
 IF NOT FOUND THEN RETURN;END IF;
 IF permitted.operation<>'semantic.local_exposure' THEN
  RETURN QUERY SELECT * FROM photo.gateway_input_before_semantic_apply(p_task,p_attempt,p_fence,p_worker,p_certificate,p_artifact);
  RETURN;
 END IF;
 RETURN QUERY SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.semantic_targets target ON target.tenant_id=t.tenant_id AND target.child_run_id=r.id
 JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id AND intent.project_id=target.project_id
 JOIN photo.v2_review_batches batch ON batch.tenant_id=intent.tenant_id AND batch.id=target.batch_id AND batch.workflow_id=intent.workflow_id
 JOIN photo.projects project ON project.tenant_id=intent.tenant_id AND project.id=intent.project_id AND project.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.v2_task_inputs input ON input.tenant_id=t.tenant_id AND input.task_id=t.id AND input.project_id=project.id
 JOIN photo.artifacts a ON a.tenant_id=input.tenant_id AND a.id=input.artifact_id AND a.sha256=input.sha256
 WHERE t.tenant_id=permitted.tenant_id AND t.id=permitted.task_id AND a.id=p_artifact
 AND t.operation='semantic.local_exposure' AND t.operation_version='1' AND t.resource_class='cpu'
 AND r.kind='apply' AND r.status IN('queued','running') AND r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot->>'project_id'=project.id::text AND target.state='queued'
 AND batch.action='apply' AND batch.child_run_id=r.id AND batch.project_id=project.id AND batch.actor_id=r.created_by
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.edit:'||target.id::text)
 AND t.node_key='semantic.edit:'||target.id::text
 AND t.execution_profile->>'renderer_version'='v2.2' AND t.execution_profile->>'backend'='cpu'
 AND t.execution_profile->>'contracts_sha'='13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5'
 AND EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation AND op.version=t.operation_version AND op.v2_enabled)
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'approved_semantic'->'nodes') node
 WHERE node->>'task_id'=t.id::text AND node->>'target_id'=target.id::text
 AND node->>'params_artifact_id'=t.params_artifact_id::text
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs params WHERE params.tenant_id=t.tenant_id AND params.task_id=t.id
 AND params.project_id=project.id AND params.artifact_id=t.params_artifact_id AND params.sha256=node->>'params_sha'))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
END $$;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) TO photo_gateway;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
