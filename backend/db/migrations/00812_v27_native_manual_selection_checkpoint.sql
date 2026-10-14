-- +goose Up
-- Immutable restart locators only; physical manual authority is separate.
ALTER TABLE photo.runs ADD COLUMN v27_initial_manual_selection text;
ALTER TABLE photo.runs ADD COLUMN v27_initial_manual_selection_sha text;
ALTER TABLE photo.runs ADD CONSTRAINT v27_initial_manual_selection_pair CHECK(
 (v27_initial_manual_selection IS NULL AND v27_initial_manual_selection_sha IS NULL)
 OR (v27_initial_manual_selection IS NOT NULL AND v27_initial_manual_selection_sha IS NOT NULL AND octet_length(v27_initial_manual_selection) BETWEEN 2 AND 65536 AND v27_initial_manual_selection_sha ~ '^[0-9a-f]{64}$'));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v27_initial_manual_selection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF current_user<>'photo_fn_owner' AND ((TG_OP='INSERT' AND (NEW.v27_initial_manual_selection IS NOT NULL OR NEW.v27_initial_manual_selection_sha IS NOT NULL))
 OR (TG_OP='UPDATE' AND (NEW.v27_initial_manual_selection IS DISTINCT FROM OLD.v27_initial_manual_selection OR NEW.v27_initial_manual_selection_sha IS DISTINCT FROM OLD.v27_initial_manual_selection_sha))) THEN
 RAISE EXCEPTION 'V27_MANUAL_SELECTION_PRIVATE' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER v27_initial_manual_selection_private BEFORE INSERT OR UPDATE OF v27_initial_manual_selection,v27_initial_manual_selection_sha ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v27_initial_manual_selection();
REVOKE ALL ON FUNCTION photo.guard_v27_initial_manual_selection() FROM PUBLIC;

CREATE FUNCTION photo.save_v27_native_manual_selection(p_tenant uuid,p_detector uuid,p_actor uuid,p_session uuid,p_config_sha text,p_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;v jsonb;sha text;native jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 native:=r.config_snapshot->'v27_detector_prepare';v:=p_body::jsonb;
 IF r.id IS NULL OR jsonb_typeof(native) IS DISTINCT FROM 'object' OR native->'schema_version' IS DISTINCT FROM '1'::jsonb OR r.id IS DISTINCT FROM photo.semantic_uuid_v5((native->>'parent_run_id')::uuid,'semantic.face.detect:reference') OR r.kind<>'v2' OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor OR r.config_hash IS DISTINCT FROM p_config_sha
 OR native->>'auth_session_id' IS DISTINCT FROM p_session::text OR photo.v2_editor_authorized(p_tenant,(r.config_snapshot->>'project_id')::uuid,p_actor,p_session) IS NOT TRUE
 OR v->'schema_version' IS DISTINCT FROM '1'::jsonb OR v->>'parent_run_id' IS DISTINCT FROM native->>'parent_run_id'
 OR v->>'request_sha' IS DISTINCT FROM native->>'request_sha' OR v->>'project_id' IS DISTINCT FROM r.config_snapshot->>'project_id'
 OR v->>'actor_id' IS DISTINCT FROM p_actor::text OR v->>'auth_session_id' IS DISTINCT FROM p_session::text OR v->'source' IS DISTINCT FROM native->'source'
 OR jsonb_typeof(v->'manual_pixels') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(v->'manual_pixels') NOT BETWEEN 1 AND 64 THEN RETURN false;END IF;
 sha:=encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex');
 IF r.v27_initial_manual_selection IS NOT NULL THEN RETURN r.v27_initial_manual_selection=p_body AND r.v27_initial_manual_selection_sha=sha;END IF;
 UPDATE photo.runs SET v27_initial_manual_selection=p_body,v27_initial_manual_selection_sha=sha WHERE tenant_id=p_tenant AND id=p_detector;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

CREATE FUNCTION photo.read_v27_native_manual_selection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_config_sha text)
RETURNS TABLE(selection_body text,selection_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.v27_initial_manual_selection,r.v27_initial_manual_selection_sha
 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference'),'face.detect') original
 JOIN photo.runs r ON r.tenant_id=p_tenant AND r.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference')
 WHERE original.config_hash=p_config_sha AND r.v27_initial_manual_selection IS NOT NULL;
$$;
ALTER FUNCTION photo.read_v27_native_manual_selection(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v27_native_manual_selection(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v27_native_manual_selection(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
