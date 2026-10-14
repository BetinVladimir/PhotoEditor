-- +goose Up
-- Immutable measured-profile identity, separate from existing immutable config.
ALTER TABLE photo.runs ADD COLUMN v26_qualification_pin jsonb;
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v26_qualification_pin() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF (TG_OP='INSERT' AND NEW.v26_qualification_pin IS NOT NULL)
 OR (TG_OP='UPDATE' AND NEW.v26_qualification_pin IS DISTINCT FROM OLD.v26_qualification_pin) THEN
  IF current_user<>'photo_fn_owner' OR NEW.kind NOT IN('bulk','v2')
   OR (TG_OP='UPDATE' AND OLD.v26_qualification_pin IS NOT NULL) THEN
   RAISE EXCEPTION 'V26_QUALIFICATION_PIN_IMMUTABLE' USING ERRCODE='42501';
  END IF;
 END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v26_qualification_pin() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v26_qualification_pin() FROM PUBLIC;
CREATE TRIGGER guard_v26_qualification_pin BEFORE INSERT OR UPDATE ON photo.runs
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v26_qualification_pin();

ALTER FUNCTION photo.v26_render_qualification_ready(uuid,uuid) RENAME TO v26_render_qualification_ready_before_pin;
REVOKE ALL ON FUNCTION photo.v26_render_qualification_ready_before_pin(uuid,uuid) FROM PUBLIC;
CREATE FUNCTION photo.v26_render_qualification_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; project uuid; current_pin record;
BEGIN
 IF NOT photo.v26_render_qualification_ready_before_pin(p_tenant,p_run) THEN RETURN false;END IF;
 SELECT r.v26_qualification_pin,w.project_id INTO pin,project
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run;
 -- Existing uncalibrated drafts retain their original qualification contract.
 IF pin IS NULL THEN RETURN true;END IF;
 SELECT * INTO current_pin FROM photo.resolve_v26_render_qualification(p_tenant,project);
 RETURN FOUND AND pin->>'model_lock_sha'=current_pin.model_lock_sha
 AND pin->>'runtime_lock_sha'=current_pin.runtime_lock_sha AND pin->>'backend'=current_pin.backend
 AND pin->>'device_profile_sha'=current_pin.device_profile_sha AND pin->>'dataset_sha'=current_pin.dataset_sha;
END $$;
ALTER FUNCTION photo.v26_render_qualification_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_qualification_ready(uuid,uuid) FROM PUBLIC;

CREATE FUNCTION photo.bind_v26_render_qualification(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_pin jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; part jsonb; child photo.runs%ROWTYPE; current_pin record;
BEGIN
 IF p_pin IS NULL OR jsonb_typeof(p_pin)<>'object' THEN RETURN 'expired';END IF;
 IF EXISTS(SELECT 1 FROM jsonb_each(p_pin) field WHERE jsonb_typeof(field.value)<>'string')
 OR p_pin-'model_lock_sha'-'runtime_lock_sha'-'backend'-'device_profile_sha'-'dataset_sha'-'calibration_sha'<>'{}'::jsonb
 OR coalesce(p_pin->>'model_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_pin->>'runtime_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_pin->>'device_profile_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_pin->>'dataset_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_pin->>'calibration_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_pin->>'backend','') NOT IN('cpu','cuda','mps') THEN RETURN 'expired';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN 'expired';END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN 'expired';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session)) THEN RETURN 'expired';END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 IF parent.id IS NULL OR parent.cancel_requested THEN RETURN 'expired';END IF;
 IF parent.v26_qualification_pin IS NOT NULL THEN
  RETURN CASE WHEN parent.v26_qualification_pin=p_pin THEN 'bound' ELSE 'conflict' END;
 END IF;
 IF parent.status<>'draft' THEN RETURN 'expired';END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO current_pin FROM photo.resolve_v26_render_qualification(p_tenant,(parent.config_snapshot->>'project_id')::uuid);
 IF NOT FOUND OR p_pin->>'model_lock_sha' IS DISTINCT FROM current_pin.model_lock_sha
 OR p_pin->>'runtime_lock_sha' IS DISTINCT FROM current_pin.runtime_lock_sha
 OR p_pin->>'backend' IS DISTINCT FROM current_pin.backend
 OR p_pin->>'device_profile_sha' IS DISTINCT FROM current_pin.device_profile_sha
 OR p_pin->>'dataset_sha' IS DISTINCT FROM current_pin.dataset_sha THEN RETURN 'unavailable';END IF;
 -- Validate every declared child before writing any pin. Readiness locks live
 -- qualification/report evidence through the caller's staging commit.
 FOR part IN SELECT value FROM jsonb_array_elements(parent.config_snapshot->'parts') ORDER BY value->>'run_id' LOOP
  SELECT * INTO child FROM photo.runs WHERE tenant_id=p_tenant AND id=(part->>'run_id')::uuid FOR UPDATE;
  IF child.id IS NULL OR child.kind<>'v2' OR child.cancel_requested OR child.graph_sealed OR child.status<>'draft'
  OR child.created_by IS DISTINCT FROM p_actor OR child.input_snapshot_id IS DISTINCT FROM parent.input_snapshot_id
  OR child.config_hash IS DISTINCT FROM part->>'config_sha'
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=child.id AND w.project_id=(parent.config_snapshot->>'project_id')::uuid)
  OR child.config_snapshot->'v26_render'->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN 'expired';END IF;
  IF child.v26_qualification_pin IS NOT NULL AND child.v26_qualification_pin<>p_pin THEN RETURN 'conflict';END IF;
  IF NOT photo.v26_render_qualification_ready(p_tenant,child.id) THEN RETURN 'unavailable';END IF;
 END LOOP;
 UPDATE photo.runs SET v26_qualification_pin=p_pin WHERE tenant_id=p_tenant
 AND (id=p_batch OR id IN(SELECT (value->>'run_id')::uuid FROM jsonb_array_elements(parent.config_snapshot->'parts')));
 RETURN 'bound';
END $$;
ALTER FUNCTION photo.bind_v26_render_qualification(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v26_render_qualification(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.bind_v26_render_qualification(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;

-- Completed legacy tickets remain readable; new completion requires an exact
-- measured-profile pin and cannot turn an uncalibrated shell into a public202.
ALTER FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) RENAME TO complete_v26_render_submission_before_pin;
REVOKE ALL ON FUNCTION photo.complete_v26_render_submission_before_pin(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.complete_v26_render_submission(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_key text,p_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb; pin jsonb;
BEGIN
 saved:=photo.read_v26_render_submission_ticket(p_tenant,p_batch,p_actor,p_session,p_key,p_sha);
 IF saved IS NOT NULL THEN RETURN saved;END IF;
 SELECT v26_qualification_pin INTO pin FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch AND kind='bulk';
 IF pin IS NULL THEN RETURN NULL;END IF;
 RETURN photo.complete_v26_render_submission_before_pin(p_tenant,p_batch,p_actor,p_session,p_key,p_sha);
END $$;
ALTER FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v26_render_submission(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
