-- +goose Up
-- Scheduler observation only, after dedicated historical receipt validation.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v29_perspective_preview_replacement_identity(p_tenant uuid,p_task uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run_id uuid;plan jsonb;packet jsonb;
BEGIN
 SELECT t.run_id INTO run_id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='optical.perspective';
 IF NOT FOUND THEN RETURN NULL;END IF;
 plan:=photo.v29_perspective_execution_plan(p_tenant,run_id);
 SELECT (r.config_snapshot->>'v29_negative_replacement_draft_jcs')::jsonb INTO packet FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=run_id;
 IF plan IS NULL OR packet IS NULL OR plan->>'task_id' IS DISTINCT FROM p_task::text THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('review_id',packet->'identity'->>'review_id','decision_hash',packet->'identity'->>'decision_hash',
 'original_task_id',packet->'identity'->>'original_task_id','original_run_id',packet->'identity'->>'original_run_id');
END $$;
ALTER FUNCTION photo.read_v29_perspective_preview_replacement_identity(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_perspective_preview_replacement_identity(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v29_perspective_preview_replacement_identity(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
