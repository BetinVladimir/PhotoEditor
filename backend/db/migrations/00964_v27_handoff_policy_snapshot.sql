-- +goose Up
-- Revalidate full saved policy catalog at all-native handoff; no data rewrite.
-- +goose StatementBegin
-- Acquire the global writer barrier before row mutation, matching readers that
-- hold the shared barrier before taking selected policy row locks.
DROP TRIGGER v27_qualification_policy_write ON photo.v2_qualification_policies;
CREATE TRIGGER v27_qualification_policy_write BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON photo.v2_qualification_policies FOR EACH STATEMENT EXECUTE FUNCTION photo.guard_v27_qualification_policy_write();
CREATE OR REPLACE FUNCTION photo.record_v27_native_initial_pipeline_handoff(p_tenant uuid,p_detector uuid,p_token uuid,p_outcome text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;parent uuid;
BEGIN
 -- Preserve admission lock order before acquiring the detector lease row.
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector;
 IF NOT FOUND THEN RETURN false;END IF;
 parent:=(r.config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 IF NOT FOUND OR p_token IS NULL OR r.v27_initial_recovery_token IS DISTINCT FROM p_token
 OR r.v27_initial_recovery_until IS NULL OR r.v27_initial_recovery_until<=clock_timestamp() THEN RETURN false;END IF;
 IF p_outcome='completed' AND r.v27_initial_manual_selection IS NULL THEN
  IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=parent AND config_snapshot ? 'v27_native_targets_transfer') THEN RETURN false;END IF;
  IF photo.v27_native_automatic_handoff_ready(p_tenant,p_detector) IS NOT TRUE THEN RETURN false;END IF;
 END IF;
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

CREATE OR REPLACE FUNCTION photo.record_v27_native_initial_pipeline_handoff(p_tenant uuid,p_detector uuid,p_token uuid,p_outcome text,p_policy_text text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;parent uuid;project uuid;session_id uuid;snapshot_sha text;marker jsonb;target jsonb;
BEGIN
 -- Preserve admission lock order before acquiring the detector lease row.
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector;
 IF NOT FOUND THEN RETURN false;END IF;
 parent:=(r.config_snapshot->'v27_detector_prepare'->>'parent_run_id')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector FOR UPDATE;
 IF NOT FOUND OR p_token IS NULL OR r.v27_initial_recovery_token IS DISTINCT FROM p_token
 OR r.v27_initial_recovery_until IS NULL OR r.v27_initial_recovery_until<=clock_timestamp() THEN RETURN false;END IF;
 IF p_outcome='completed' AND r.v27_initial_manual_selection IS NULL THEN
  SELECT config_snapshot->'v27_native_targets_transfer' INTO marker FROM photo.runs WHERE tenant_id=p_tenant AND id=parent;
  IF marker IS NOT NULL THEN
   project:=(r.config_snapshot->>'project_id')::uuid;
   session_id:=(r.config_snapshot->'v27_detector_prepare'->>'auth_session_id')::uuid;
   PERFORM set_config('app.tenant_id',p_tenant::text,true);
   IF p_policy_text IS NULL OR octet_length(p_policy_text) NOT BETWEEN 2 AND 4194304 THEN RETURN false;END IF;
   snapshot_sha:=encode(public.digest(convert_to(p_policy_text,'UTF8'),'sha256'),'hex');
   -- Full original catalog hash is distinct from the parent transfer policy fingerprint.
   PERFORM w.id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_detector AND w.project_id=project AND w.policy_sha=snapshot_sha FOR SHARE;
   IF NOT FOUND THEN RETURN false;END IF;
   IF photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,project,r.created_by,session_id,p_policy_text::jsonb) IS NOT TRUE THEN RETURN false;END IF;
   PERFORM w.id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=photo.semantic_uuid_v5(parent,'semantic.defect.prepare:reference') AND w.project_id=project AND w.policy_sha=snapshot_sha FOR SHARE;
   IF NOT FOUND THEN RETURN false;END IF;
   FOR target IN SELECT value FROM jsonb_array_elements(marker->'targets') WHERE value->'reuse_reference'='false'::jsonb LOOP
    PERFORM w.id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=(target->'producer'->>'run_id')::uuid AND w.project_id=project AND w.policy_sha=snapshot_sha FOR SHARE;
    IF NOT FOUND THEN RETURN false;END IF;
   END LOOP;
   IF photo.lock_semantic_proposal_qualification(p_tenant,project,r.created_by,session_id,
    (SELECT config_snapshot->'v27_semantic'->'qualification' FROM photo.runs WHERE tenant_id=p_tenant AND id=parent)) IS NOT TRUE THEN RETURN false;END IF;
  END IF;
  IF photo.v27_native_automatic_handoff_ready(p_tenant,p_detector) IS NOT TRUE THEN RETURN false;END IF;
 END IF;
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
ALTER FUNCTION photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
