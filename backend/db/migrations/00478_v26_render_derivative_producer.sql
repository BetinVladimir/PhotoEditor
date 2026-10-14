-- +goose Up
-- Poll committed adoption evidence; enqueue repeats authority under locks.
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_next_v26_render_derivative(p_srgb_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;intent uuid;
BEGIN
 IF p_srgb_sha IS NULL OR p_srgb_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT t.tenant_id,t.id,s.result_id INTO candidate FROM photo.tasks t
 CROSS JOIN LATERAL photo.v26_render_derivative_source(t.tenant_id,t.id) s
 WHERE t.operation='render.compose' AND t.operation_version='1' AND t.state='succeeded'
 AND NOT EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=t.tenant_id AND r.v26_derivative_task_id=t.id)
 ORDER BY t.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 intent:=photo.enqueue_v26_render_derivative(candidate.tenant_id,candidate.id,candidate.result_id,p_srgb_sha);
 RETURN intent IS NOT NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_next_v26_render_derivative(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_next_v26_render_derivative(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_next_v26_render_derivative(text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
