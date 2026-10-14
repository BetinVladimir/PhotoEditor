-- +goose Up
-- Original Params discovery only for current completed perspective tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_adoption_params(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS TABLE(artifact_id uuid,sha256 text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256 AND i.contract='PerspectiveLineDetectionParams'
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='optical.perspective_lines' AND t.operation_version='1'
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_adoption_context(p_tenant,p_task));
$$;
ALTER FUNCTION photo.v29_perspective_lines_adoption_params(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_adoption_params(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_adoption_params(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
