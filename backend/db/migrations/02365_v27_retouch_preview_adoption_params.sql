-- +goose Up
-- Exact canonical generation Params, re-resolved during preview output adoption.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_retouch_preview_adoption_params(p_tenant uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_result uuid) RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.semantic_intents i ON i.tenant_id=t.tenant_id
  AND i.id::text=r.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
  AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='semantic.preview_retouch_region'
 AND t.operation_version='1' AND t.resource_class='cpu' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND z.id=p_result
 AND r.kind='preview' AND r.graph_sealed AND NOT r.cancel_requested AND r.status IN('running','completed')
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.bytes BETWEEN 2 AND 1048576 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.semantic_retouch_preview_bundle_ready(t.id,z.id,z.attempt,z.fencing_token) IS TRUE
 AND photo.semantic_retouch_preview_run_current(t.tenant_id,r.id) IS TRUE
 AND photo.v27_downstream_profile_ready(t.tenant_id,i.project_id,t.operation,t.operation_version,'cpu',
  r.v27_preapply_preview_profile) IS TRUE
 AND EXISTS(SELECT 1 FROM photo.v2_task_inputs x WHERE x.tenant_id=t.tenant_id AND x.task_id=t.id
  AND x.artifact_id=a.id AND x.sha256=a.sha256 AND x.contract='Params')
 AND EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task));
$$;
ALTER FUNCTION photo.v27_retouch_preview_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_retouch_preview_adoption_params(uuid,uuid,integer,bigint,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.v27_retouch_preview_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
