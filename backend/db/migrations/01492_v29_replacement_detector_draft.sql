-- +goose Up
-- Preliminary draft only. Existing negative marker blocks generic admission.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v29_replacement_detector_draft() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.config_snapshot ? 'v29_perspective_replacement_detection_jcs' AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'Replacement detector requires dedicated authority' USING ERRCODE='42501';END IF;
 ELSIF OLD.config_snapshot ? 'v29_perspective_replacement_detection_jcs' THEN
  IF ROW(NEW.id,NEW.tenant_id,NEW.kind,NEW.created_by,NEW.input_snapshot_id,NEW.config_snapshot,NEW.config_hash,NEW.failure_policy)
   IS DISTINCT FROM ROW(OLD.id,OLD.tenant_id,OLD.kind,OLD.created_by,OLD.input_snapshot_id,OLD.config_snapshot,OLD.config_hash,OLD.failure_policy) THEN
   RAISE EXCEPTION 'Replacement detector draft is immutable' USING ERRCODE='42501';END IF;
 ELSIF NEW.config_snapshot ? 'v29_perspective_replacement_detection_jcs' THEN
  RAISE EXCEPTION 'Replacement detector cannot replace an existing run' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v29_replacement_detector_draft() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v29_replacement_detector_draft() FROM PUBLIC;
CREATE TRIGGER runs_v29_replacement_detector_draft_guard BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v29_replacement_detector_draft();
CREATE FUNCTION photo.save_v29_replacement_detector_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_descriptor text,p_original text,p_snapshot uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;config jsonb;detector_run uuid;old photo.runs%ROWTYPE;
BEGIN
 IF photo.lock_v29_replacement_detector_preparation(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_descriptor,p_original,p_snapshot) IS DISTINCT FROM true THEN RETURN false;END IF;
 d:=p_descriptor::jsonb;detector_run:=(d->>'run_id')::uuid;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=(d->'base'->'source'->>'artifact_id')::uuid
 AND a.sha256=d->'base'->'source'->>'sha256' AND a.bytes=octet_length(d->>'source_jcs')
 AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 config:=jsonb_build_object('project_id',p_project,'v29_negative_replacement',
 jsonb_build_object('version','v29_perspective_replacement_detection_v1','project_id',p_project,'review_id',p_review,
 'decision_hash',p_hash,'original_task_id',d->'replacement'->>'original_task_id','original_run_id',d->'replacement'->>'original_run_id'),
 'v29_perspective_replacement_detection_jcs',p_descriptor);
 SELECT * INTO old FROM photo.runs WHERE id=detector_run FOR UPDATE;
 IF FOUND THEN RETURN old.tenant_id=p_tenant AND old.kind='v2' AND old.status='draft' AND old.created_by=p_actor
 AND old.input_snapshot_id=p_snapshot AND old.failure_policy='fail_fast' AND NOT old.graph_sealed AND NOT old.cancel_requested
 AND old.config_snapshot=config AND old.config_hash=encode(sha256(convert_to(config::text,'UTF8')),'hex')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=detector_run);END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(detector_run,p_tenant,'v2','draft',p_snapshot,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),p_actor,'fail_fast',false);
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.save_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
