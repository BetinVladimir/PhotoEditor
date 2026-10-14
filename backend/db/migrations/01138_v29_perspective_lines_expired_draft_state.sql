-- +goose Up
-- Distinguish proven pre-admission expiry from missing authorization, stale
-- source, and legacy admitted runs without a persisted response.
-- +goose StatementBegin
ALTER FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) RENAME TO v29_perspective_lines_submission_state_pre_expiry;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_submission_state_pre_expiry(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v29_perspective_lines_submission_state(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_snapshot uuid,p_base jsonb,p_policy text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE state jsonb;r photo.runs%ROWTYPE;
BEGIN
 state:=photo.v29_perspective_lines_submission_state_pre_expiry(p_tenant,p_project,p_run,p_actor,p_session,p_snapshot,p_base,p_policy);
 IF state IS NULL OR state->>'stage' IS DISTINCT FROM 'expired' THEN RETURN state;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR r.status<>'cancelled' OR NOT r.cancel_requested OR r.graph_sealed
 OR r.v29_perspective_lines_admission_receipt IS NOT NULL THEN RETURN state;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.run_events WHERE tenant_id=p_tenant AND run_id=p_run
 AND type='v2.run.cancelled' AND payload->>'reason'='draft_expired')
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
 AND (attempt<>0 OR canonical_result_id IS NOT NULL OR state<>'cancelled'
 OR terminal_reason IS DISTINCT FROM 'v29_lines_draft_expired')) THEN RETURN state;END IF;
 RETURN jsonb_build_object('stage','draft_expired','task_id',state->'task_id');
END $$;
ALTER FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
