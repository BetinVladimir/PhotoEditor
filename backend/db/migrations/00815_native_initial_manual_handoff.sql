-- +goose Up
-- Producer readiness completes only after the explicit manual branch has
-- committed complete child admission and enabled durable parent recovery.
-- +goose StatementBegin
CREATE FUNCTION photo.record_v27_native_initial_pipeline_handoff(p_tenant uuid,p_detector uuid,p_token uuid,p_outcome text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;parent uuid;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 IF NOT FOUND OR p_token IS NULL OR r.v27_initial_recovery_token IS DISTINCT FROM p_token
 OR r.v27_initial_recovery_until IS NULL OR r.v27_initial_recovery_until<=clock_timestamp() THEN RETURN false;END IF;
 IF p_outcome='completed' AND r.v27_initial_manual_selection IS NOT NULL THEN
  IF NOT r.v27_parent_recovery_enabled OR r.v27_initial_manual_selection_sha IS DISTINCT FROM encode(public.digest(convert_to(r.v27_initial_manual_selection,'UTF8'),'sha256'),'hex') THEN RETURN false;END IF;
  parent:=(r.config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid;
  PERFORM set_config('app.tenant_id',p_tenant::text,true);
  IF NOT EXISTS(
   SELECT 1 FROM photo.semantic_native_input_saved_stage(p_tenant,(r.config_snapshot->>'project_id')::uuid,r.created_by,
    (r.config_snapshot->'v27_detector_prepare'->>'auth_session_id')::uuid,parent,
    photo.semantic_uuid_v5(parent,'semantic.defect.prepare:reference'),'defects.propose') saved
   JOIN photo.runs child ON child.tenant_id=p_tenant AND child.id=photo.semantic_uuid_v5(parent,'semantic.defect.prepare:reference')
   WHERE child.graph_sealed AND child.config_snapshot->'v27_defect_native_prepare'->'request_sha'=r.config_snapshot->'v27_detector_prepare'->'request_sha'
  ) THEN RETURN false;END IF;
 END IF;
 RETURN photo.record_v27_native_initial_pipeline(p_tenant,p_detector,p_token,p_outcome);
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
