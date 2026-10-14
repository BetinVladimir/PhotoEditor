-- +goose Up
-- Only an unmaterialized single manual mask job may fail at vector preflight.
-- +goose StatementBegin
CREATE FUNCTION photo.fail_interactive_manual_preflight(p_tenant uuid,p_run uuid,p_task uuid,p_token uuid,p_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.interactive_tasks%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_code IS NULL OR p_code NOT IN('MANUAL_TEMPLATE_INVALID','MANUAL_GEOMETRY_INVALID') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'mask' OR r.graph_sealed OR r.cancel_requested OR r.status NOT IN('queued','running') THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.interactive_tasks WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=p_task FOR UPDATE;
 IF NOT FOUND OR j.kind<>'mask_manual' OR j.operation<>'mask.rasterize' OR j.state<>'building'
 OR j.lease_token IS DISTINCT FROM p_token OR j.lease_until IS NULL OR j.lease_until<=clock_timestamp()
 OR (SELECT count(*) FROM photo.interactive_tasks other WHERE other.tenant_id=p_tenant AND other.run_id=p_run)<>1
 OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run) THEN RETURN false;END IF;
 UPDATE photo.interactive_tasks SET state='failed',last_error=p_code,lease_token=NULL,lease_until=NULL WHERE tenant_id=p_tenant AND task_id=p_task;
 UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
ALTER FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
