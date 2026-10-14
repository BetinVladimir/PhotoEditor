-- +goose Up
-- An old validator result cannot fail a concurrently changed manual payload.
-- +goose StatementBegin
CREATE FUNCTION photo.fail_interactive_manual_preflight(p_tenant uuid,p_run uuid,p_task uuid,p_token uuid,p_params_sha text,p_params jsonb,p_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.interactive_tasks%ROWTYPE;
BEGIN
 IF p_params_sha IS NULL OR p_params_sha !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR octet_length(p_params::text) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 -- Same lock order as the terminal implementation: run, then manual job.
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.interactive_tasks WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=p_task FOR UPDATE;
 IF NOT FOUND OR j.params_sha256 IS DISTINCT FROM p_params_sha OR j.params IS DISTINCT FROM p_params THEN RETURN false;END IF;
 RETURN photo.fail_interactive_manual_preflight(p_tenant,p_run,p_task,p_token,p_code);
END $$;
ALTER FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text,jsonb,text) TO photo_scheduler;
REVOKE ALL ON FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
