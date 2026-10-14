-- +goose Up
-- Only the handoff wrapper may invoke the producer-only record implementation.
-- +goose StatementBegin
REVOKE ALL ON FUNCTION photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text) FROM PUBLIC,photo_scheduler;
CREATE OR REPLACE FUNCTION photo.save_v27_native_manual_selection(p_tenant uuid,p_detector uuid,p_actor uuid,p_session uuid,p_config_sha text,p_body text) RETURNS boolean
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
 -- Replays of an already selected immutable value remain idempotent. A new
 -- branch cannot be attached after initial or parent scheduling has started.
 IF r.v27_initial_recovery_done OR r.v27_parent_recovery_enabled OR r.v27_initial_recovery_token IS NOT NULL THEN RETURN false;END IF;
 UPDATE photo.runs SET v27_initial_manual_selection=p_body,v27_initial_manual_selection_sha=sha WHERE tenant_id=p_tenant AND id=p_detector;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v27_native_manual_selection(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
