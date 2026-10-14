-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_exposure_presentation_evidence(p_tenant uuid,p_target uuid,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,role text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,a.output_role
 FROM photo.semantic_exposure_presentation_source(p_tenant,p_target) source
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=p_artifact
 AND a.created_by_task_id=source.task_id AND a.produced_attempt=source.attempt AND a.produced_fence=source.fence
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((a.output_role IN('document','working_image','report') AND a.media_type='application/json')
 OR (a.output_role='pixels' AND a.media_type='application/x-npy'));
$$;
ALTER FUNCTION photo.semantic_exposure_presentation_evidence(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_exposure_presentation_evidence(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_exposure_presentation_evidence(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
