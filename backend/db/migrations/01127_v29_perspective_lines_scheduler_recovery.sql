-- +goose Up
-- Read-only recovery material; this is never a cached source permission.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_scheduler_recovery(p_tenant uuid,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.cancel_requested
 OR r.status NOT IN('draft','queued','running','waiting_review','completed')
 OR r.v29_perspective_lines_seal_receipt IS NULL
 OR r.v29_perspective_lines_execution_policy IS NULL THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,
 'intent',r.config_snapshot->'v29_perspective_line_detection',
 'policy_jcs',r.v29_perspective_lines_execution_policy);
END $$;
ALTER FUNCTION photo.v29_perspective_lines_scheduler_recovery(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_scheduler_recovery(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_scheduler_recovery(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
