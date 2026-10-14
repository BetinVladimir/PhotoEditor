-- +goose Up
-- The acceptance response is immutable; current task status is read separately.
ALTER TABLE photo.runs ADD COLUMN v29_replacement_detector_admission_receipt jsonb;
ALTER TABLE photo.runs ADD CONSTRAINT runs_v29_replacement_detector_admission_receipt_scope CHECK(
 v29_replacement_detector_admission_receipt IS NULL OR ((config_snapshot ? 'v29_perspective_replacement_detection_jcs')
 AND graph_sealed AND jsonb_typeof(v29_replacement_detector_admission_receipt)='object'
 AND pg_column_size(v29_replacement_detector_admission_receipt)<=2048));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_replacement_detector_admission_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.v29_replacement_detector_admission_receipt IS NOT NULL THEN RAISE EXCEPTION 'Admission receipt requires activation' USING ERRCODE='42501';END IF;
 ELSIF NEW.v29_replacement_detector_admission_receipt IS DISTINCT FROM OLD.v29_replacement_detector_admission_receipt THEN
  IF current_user<>'photo_fn_owner' OR OLD.v29_replacement_detector_admission_receipt IS NOT NULL
   OR NOT (OLD.config_snapshot ? 'v29_perspective_replacement_detection_jcs') OR NOT OLD.graph_sealed
   OR OLD.status NOT IN('queued','running','waiting_review','completed') OR OLD.cancel_requested
   OR ROW(NEW.status,NEW.graph_sealed,NEW.cancel_requested) IS DISTINCT FROM ROW(OLD.status,OLD.graph_sealed,OLD.cancel_requested)
  THEN RAISE EXCEPTION 'Admission receipt is immutable' USING ERRCODE='42501';END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_replacement_detector_admission_receipt() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_replacement_detector_admission_receipt() FROM PUBLIC;
CREATE TRIGGER runs_v29_replacement_detector_admission_receipt_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_replacement_detector_admission_receipt();

ALTER FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) RENAME TO activate_v29_replacement_detector_pre_receipt;
REVOKE ALL ON FUNCTION photo.activate_v29_replacement_detector_pre_receipt(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.activate_v29_replacement_detector(p_tenant uuid,p_run uuid,p_proof jsonb,p_witness jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt jsonb;
BEGIN
 IF photo.activate_v29_replacement_detector_pre_receipt(p_tenant,p_run,p_proof,p_witness) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT jsonb_build_object('run_id',r.id,'workflow_id',w.id,'task_id',((r.config_snapshot->>'v29_perspective_replacement_detection_jcs')::jsonb->>'task_id')::uuid,'state','queued') INTO receipt
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND (r.config_snapshot ? 'v29_perspective_replacement_detection_jcs') AND r.graph_sealed;
 IF receipt IS NULL THEN RAISE EXCEPTION 'LINE_ADMISSION_RECEIPT_MISSING_WORKFLOW' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET v29_replacement_detector_admission_receipt=receipt
 WHERE tenant_id=p_tenant AND id=p_run AND v29_replacement_detector_admission_receipt IS NULL;
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
 AND v29_replacement_detector_admission_receipt IS DISTINCT FROM receipt) THEN
  RAISE EXCEPTION 'LINE_ADMISSION_RECEIPT_CONFLICT' USING ERRCODE='23514';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.activate_v29_replacement_detector(uuid,uuid,jsonb,jsonb) TO photo_scheduler;

-- +goose StatementEnd
-- +goose StatementBegin
-- Read-only historical admission. No fresh source permission, quota renewal,
-- task transition or outbox insertion; current run status is a separate concern.
CREATE FUNCTION photo.read_v29_replacement_detector_admission(p_tenant uuid,p_run uuid,p_descriptor text,p_snapshot uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;d jsonb;expected jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 3145728 THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT FOUND OR r.input_snapshot_id IS DISTINCT FROM p_snapshot OR NOT r.graph_sealed OR r.v29_replacement_detector_admission_receipt IS NULL
 OR r.config_snapshot->>'v29_perspective_replacement_detection_jcs' IS DISTINCT FROM p_descriptor THEN RETURN NULL;END IF;
 d:=p_descriptor::jsonb;
 IF d->>'tenant_id' IS DISTINCT FROM p_tenant::text OR d->>'run_id' IS DISTINCT FROM p_run::text
 OR d->>'actor_id' IS DISTINCT FROM r.created_by::text THEN RETURN NULL;END IF;
 PERFORM id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND id=photo.v30_uuid5(p_run,'workflow')
 AND project_id=(d->>'project_id')::uuid AND input_snapshot_id=r.input_snapshot_id
 AND workflow_type='preview' AND renderer_version='v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 expected:=jsonb_build_object('run_id',p_run,'workflow_id',photo.v30_uuid5(p_run,'workflow'),'task_id',(d->>'task_id')::uuid,'state','queued');
 IF r.v29_replacement_detector_admission_receipt IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 RETURN expected;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v29_replacement_detector_admission(uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v29_replacement_detector_admission(uuid,uuid,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.read_v29_replacement_detector_admission(uuid,uuid,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
