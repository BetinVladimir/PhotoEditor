-- +goose Up
-- One fresh semantic preview per scheduler iteration; recheck locked admission.
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_next_semantic_exposure_preview(p_srgb_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record; intent uuid;
BEGIN
 IF p_srgb_sha IS NULL OR p_srgb_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT target.tenant_id,target.id,s.result_id INTO candidate
 FROM photo.semantic_targets target
 CROSS JOIN LATERAL photo.semantic_exposure_presentation_source(target.tenant_id,target.id) s
 WHERE target.state='applied'
 AND NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=target.tenant_id AND r.v26_derivative_task_id=s.task_id)
 ORDER BY target.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 intent:=photo.enqueue_semantic_exposure_preview(candidate.tenant_id,candidate.id,candidate.result_id,p_srgb_sha);
 RETURN intent IS NOT NULL;
END $$;
ALTER FUNCTION photo.enqueue_next_semantic_exposure_preview(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_next_semantic_exposure_preview(text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.enqueue_next_semantic_exposure_preview(text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
