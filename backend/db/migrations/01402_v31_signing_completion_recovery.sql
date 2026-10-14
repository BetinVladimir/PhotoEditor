-- +goose Up
-- Read a committed acknowledgement without depending on live SDK/key rotation.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_adoption_committed(p_tenant uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.provenance_manifests m ON m.tenant_id=w.tenant_id AND m.project_id=w.project_id AND m.workflow_id=w.id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id AND d.purpose='task_params' AND d.artifact_id=t.params_artifact_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='provenance.sign' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND r.kind='v2' AND r.status IN('running','completed')
 AND r.graph_sealed AND NOT r.cancel_requested AND w.workflow_type='provenance' AND w.renderer_version='v2.2'
 AND tenant.status='active' AND project.archived_at IS NULL AND project.download_allowed
 AND d.v31_signing_session_id IS NOT NULL AND photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,d.v31_signing_session_id)
 AND m.export_id::text=r.config_snapshot->>'export_id'
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND (SELECT count(*) FROM photo.v2_artifact_links l JOIN photo.artifacts a ON a.tenant_id=l.tenant_id AND a.id=l.artifact_id AND a.sha256=l.sha256
  WHERE l.tenant_id=p_tenant AND l.workflow_id=w.id AND l.project_id=w.project_id
  AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
  AND ((l.ordinal=0 AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256' AND a.output_role='document')
   OR (l.ordinal=1 AND a.id=m.signed_artifact_id AND a.output_role='signed_export')
   OR (l.ordinal=2 AND a.id=m.manifest_artifact_id AND a.output_role='manifest')))=3);
$$;
ALTER FUNCTION photo.v31_signing_adoption_committed(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_adoption_committed(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_signing_adoption_committed(uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) RENAME TO finalize_v2_run_pre_v31_signing;
REVOKE ALL ON FUNCTION photo.finalize_v2_run_pre_v31_signing(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tid uuid;
BEGIN
 IF EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run AND w.workflow_type='provenance'
  AND r.status IN('queued','running') AND NOT r.cancel_requested)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state IN('failed','cancelled','skipped')) THEN
  FOR tid IN SELECT id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state='succeeded' LOOP
   IF photo.v31_signing_adoption_committed(p_tenant,tid) IS DISTINCT FROM true THEN RETURN 'pending';END IF;
  END LOOP;
 END IF;
 RETURN photo.finalize_v2_run_pre_v31_signing(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
