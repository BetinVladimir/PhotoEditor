-- +goose Up
-- The acceptance response is immutable; current task status is read separately.
ALTER TABLE photo.runs ADD COLUMN v29_perspective_lines_admission_receipt jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_lines_admission_receipt_scope CHECK(
 v29_perspective_lines_admission_receipt IS NULL OR (v29_perspective_line_detection
 AND graph_sealed AND jsonb_typeof(v29_perspective_lines_admission_receipt)='object'
 AND pg_column_size(v29_perspective_lines_admission_receipt)<=2048));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_lines_admission_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_perspective_lines_admission_receipt IS NOT NULL THEN RAISE EXCEPTION 'Admission receipt requires activation' USING ERRCODE='42501';END IF;
 ELSIF NEW.v29_perspective_lines_admission_receipt IS DISTINCT FROM OLD.v29_perspective_lines_admission_receipt THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_perspective_lines_admission_receipt IS NOT NULL
   OR NOT OLD.v29_perspective_line_detection OR NOT OLD.graph_sealed
   OR OLD.status NOT IN('queued','running','waiting_review','completed') OR OLD.cancel_requested
   OR ROW(NEW.status,NEW.graph_sealed,NEW.cancel_requested) IS DISTINCT FROM ROW(OLD.status,OLD.graph_sealed,OLD.cancel_requested)
  THEN RAISE EXCEPTION 'Admission receipt is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_lines_admission_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_lines_admission_receipt() FROM PUBLIC;
CREATE TRIGGER runs_v29_lines_admission_receipt_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_lines_admission_receipt();

ALTER FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) RENAME TO activate_v29_perspective_lines_pre_receipt;
REVOKE ALL ON FUNCTION photo.activate_v29_perspective_lines_pre_receipt(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.activate_v29_perspective_lines(p_tenant uuid,p_run uuid,p_witness jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;
BEGIN
 IF photo.activate_v29_perspective_lines_pre_receipt(p_tenant,p_run,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT jsonb_build_object('run_id',r.id,'workflow_id',w.id,'task_id',(r.config_snapshot->'v29_perspective_line_detection'->>'task_id')::uuid,'state','queued') INTO receipt
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.v29_perspective_line_detection AND r.graph_sealed;
 IF receipt IS NULL THEN RAISE EXCEPTION 'LINE_ADMISSION_RECEIPT_MISSING_WORKFLOW' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET v29_perspective_lines_admission_receipt=receipt
 WHERE tenant_id=p_tenant AND id=p_run AND v29_perspective_lines_admission_receipt IS NULL;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
 AND v29_perspective_lines_admission_receipt IS DISTINCT FROM receipt) THEN
  RAISE EXCEPTION 'LINE_ADMISSION_RECEIPT_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.activate_v29_perspective_lines(uuid,uuid,jsonb) TO photo_scheduler;

ALTER FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) RENAME TO v29_perspective_lines_submission_state_pre_receipt;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_submission_state_pre_receipt(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v29_perspective_lines_submission_state(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_snapshot uuid,p_base jsonb,p_policy text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE state jsonb;receipt jsonb;
BEGIN
 state:=photo.v29_perspective_lines_submission_state_pre_receipt(p_tenant,p_project,p_run,p_actor,p_session,p_snapshot,p_base,p_policy);
 IF state IS NULL OR state->>'stage' IN('missing','conflict','draft','sealed') THEN RETURN state;END IF;
 SELECT v29_perspective_lines_admission_receipt INTO receipt FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF receipt IS NULL THEN RETURN state;END IF;
 -- This is the original acceptance acknowledgement, including after failure or
 -- cancellation. It performs no activation, source IO, or reservation writes.
 RETURN jsonb_build_object('stage','queued','task_id',receipt->'task_id');
END $$;
ALTER FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
