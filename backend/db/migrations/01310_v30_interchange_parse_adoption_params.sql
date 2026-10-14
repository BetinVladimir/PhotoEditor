-- +goose Up
-- Exact current parse Params read for the independent scheduler verifier.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_interchange_parse_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params'
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
 JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='Params'
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id
 JOIN photo.projects project ON project.tenant_id=w.tenant_id AND project.id=w.project_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='v2.interchange.parse' AND t.operation_version='1'
 AND t.resource_class='cpu' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result
 AND tenant.status='active' AND project.archived_at IS NULL AND project.download_allowed
 AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND w.workflow_type='interchange' AND w.renderer_version='v2.2'
 AND d.state='ready' AND d.v30_interchange_proof->>'project_id'=w.project_id::text
 AND photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,(d.v30_interchange_proof->>'auth_session_id')::uuid)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes=d.bytes AND a.bytes BETWEEN 2 AND 65536
 AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v2_input_closure_ready(t.id)
 AND EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task))
$$;
ALTER FUNCTION photo.v30_interchange_parse_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_interchange_parse_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_interchange_parse_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
