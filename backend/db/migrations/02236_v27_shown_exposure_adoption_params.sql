-- +goose Up
-- Result-scoped Params lookup only; this does not authorize final adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_exposure_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='apply'
 AND r.graph_sealed AND NOT r.cancel_requested AND r.status IN('queued','running')
 JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id=(r.config_snapshot->>'project_id')::uuid AND p.archived_at IS NULL
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.task_id=t.id
 AND result.id=t.canonical_result_id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='semantic.adopt_shown_exposure'
 AND t.operation_version='1' AND t.resource_class='cpu' AND t.state='succeeded'
 AND NOT t.cancel_requested AND t.attempt=p_attempt AND t.fencing_token=p_fence AND result.id=p_result
 AND t.execution_profile->>'backend'='cpu' AND t.execution_profile->>'renderer_version'='v2.2'
 AND jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes')='array'
 AND (SELECT count(*) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes')='array'
 THEN r.config_snapshot->'shown_semantic'->'nodes' ELSE '[]'::jsonb END) n
 WHERE n->>'task_id'=t.id::text AND n->>'params_artifact_id'=a.id::text AND n->>'params_sha'=a.sha256)=1;
$$;
ALTER FUNCTION photo.v27_shown_exposure_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_exposure_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_shown_exposure_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
