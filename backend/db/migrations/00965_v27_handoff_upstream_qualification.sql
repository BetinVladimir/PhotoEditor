-- +goose Up
-- Exact saved upstream reports remain admission dependencies through handoff.
-- No data rewrite, new application DML grant or object publication.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v27_native_handoff_qualification_chain(p_tenant uuid,p_parent uuid,p_defect uuid,p_person uuid,p_project uuid,p_actor uuid,p_session uuid,p_policy_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.runs%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;c jsonb;n jsonb;skin jsonb;landmarks jsonb;parser jsonb;
 prefix text;defect_key text;role record;qualified record;matches integer;ids uuid[];report jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.runs WHERE tenant_id=p_tenant AND id=p_defect FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_defect=photo.semantic_uuid_v5(p_parent,'semantic.defect.prepare:reference') THEN
  prefix:='v27_';defect_key:='v27_defect_native_prepare';
  IF p_person IS DISTINCT FROM photo.semantic_uuid_v5(p_parent,'semantic.mask.person:reference') THEN RETURN false;END IF;
 ELSE
  prefix:='v27_target_';defect_key:='v27_target_defect_prepare';
 END IF;
 c:=d.config_snapshot->defect_key;
 IF jsonb_typeof(c) IS DISTINCT FROM 'object' OR c->>'parent_run_id' IS DISTINCT FROM p_parent::text
 OR (prefix='v27_target_' AND c->>'person_run_id' IS DISTINCT FROM p_person::text) THEN RETURN false;END IF;
 SELECT config_snapshot->(prefix||'skin_prepare') INTO skin FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'skin_run_id')::uuid;
 SELECT config_snapshot->(prefix||'face_prepare') INTO landmarks FROM photo.runs WHERE tenant_id=p_tenant AND id=(c->>'landmarks_run_id')::uuid;
 SELECT config_snapshot->(prefix||'face_prepare') INTO parser FROM photo.runs WHERE tenant_id=p_tenant AND id=(skin->>'parser_run_id')::uuid;
 IF jsonb_typeof(skin) IS DISTINCT FROM 'object' OR jsonb_typeof(landmarks) IS DISTINCT FROM 'object'
 OR jsonb_typeof(parser) IS DISTINCT FROM 'object' OR parser->>'detector_run_id' IS DISTINCT FROM landmarks->>'detector_run_id' THEN RETURN false;END IF;
 ids:=ARRAY[p_defect,p_person,(c->>'landmarks_run_id')::uuid,(c->>'skin_run_id')::uuid,(skin->>'parser_run_id')::uuid,(landmarks->>'detector_run_id')::uuid];
 IF (SELECT count(DISTINCT id) FROM unnest(ids) id)<>6 THEN RETURN false;END IF;
 -- Deterministic order within each fixed six-stage chain.
 FOR role IN SELECT * FROM (VALUES
  (ids[1],'defects.propose',defect_key),
  (ids[2],'mask.person',prefix||'person_prepare'),
  (ids[3],'face.landmarks',prefix||'face_prepare'),
  (ids[4],'mask.face_skin',prefix||'skin_prepare'),
  (ids[5],'face.parse',prefix||'face_prepare'),
  (ids[6],'face.detect',prefix||'detector_prepare')) x(id,operation,config_key) ORDER BY id LOOP
  SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=role.id FOR SHARE;
  IF NOT FOUND OR NOT r.graph_sealed OR r.cancel_requested OR r.kind<>'v2'
  OR r.status NOT IN('running','waiting_review','completed') OR r.created_by IS DISTINCT FROM p_actor
  OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text THEN RETURN false;END IF;
  n:=r.config_snapshot->role.config_key;
  IF jsonb_typeof(n) IS DISTINCT FROM 'object' OR n->>'parent_run_id' IS DISTINCT FROM p_parent::text
  OR n->>'auth_session_id' IS DISTINCT FROM p_session::text OR n->'source' IS DISTINCT FROM c->'source'
  OR n->>'request_sha' IS DISTINCT FROM c->>'request_sha' OR n->>'policy_sha' IS DISTINCT FROM p_policy_sha THEN RETURN false;END IF;
  PERFORM w.id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=r.id AND w.project_id=p_project AND w.policy_sha=p_policy_sha FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=(n->>'task_id')::uuid FOR SHARE;
  IF NOT FOUND OR t.operation<>role.operation OR t.operation_version<>'1' OR t.state<>'succeeded' OR t.cancel_requested
  OR t.input_digest IS DISTINCT FROM n->>'input_digest' THEN RETURN false;END IF;
  PERFORM z.id FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=t.id AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
  -- face_skin is deterministic and has no model qualification report.
  IF role.operation='mask.face_skin' THEN CONTINUE;END IF;
  report:=n->'recovery'->'qualification_report';matches:=0;
  IF jsonb_typeof(report) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  FOR qualified IN SELECT q.id,policy.id AS policy_id FROM photo.model_qualifications q
   JOIN photo.v2_qualification_policies policy ON policy.operation=q.operation AND policy.operation_version=q.operation_version
    AND policy.model_lock_sha=q.model_lock_sha AND policy.runtime_lock_sha=q.runtime_lock_sha AND policy.backend=q.backend
    AND policy.device_profile_sha=q.device_profile_sha AND policy.dataset_sha=q.dataset_sha
   JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
   WHERE q.tenant_id=p_tenant AND q.project_id=p_project AND q.operation=role.operation AND q.operation_version='1'
    AND q.trusted_import AND q.status='passed' AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
    AND a.id::text=report->>'artifact_id' AND a.sha256=report->>'sha256'
    AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
    AND policy.backend=t.execution_profile->>'backend'
    AND (role.operation='defects.propose' OR NOT(t.execution_profile ?| ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha']) OR (policy.model_lock_sha=t.execution_profile->>'model_lock_sha'
      AND policy.runtime_lock_sha=t.execution_profile->>'runtime_lock_sha' AND policy.device_profile_sha=t.execution_profile->>'device_profile_sha'))
   FOR SHARE OF q,policy,a LOOP
    PERFORM log.id FROM photo.audit_log log WHERE log.tenant_id=p_tenant AND log.action='v2_qualification.imported'
     AND log.resource_type='model_qualification' AND log.resource_id=qualified.id AND log.details->>'policy_id'=qualified.policy_id::text ORDER BY log.id LIMIT 1 FOR SHARE;
    IF NOT FOUND THEN RETURN false;END IF;
    matches:=matches+1;
  END LOOP;
  IF matches<>1 THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v27_native_handoff_qualification_chain(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v27_native_handoff_qualification_chain(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;

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
   IF photo.lock_v27_native_handoff_qualification_chain(p_tenant,parent,photo.semantic_uuid_v5(parent,'semantic.defect.prepare:reference'),photo.semantic_uuid_v5(parent,'semantic.mask.person:reference'),project,r.created_by,session_id,snapshot_sha) IS NOT TRUE THEN RETURN false;END IF;
   FOR target IN SELECT value FROM jsonb_array_elements(marker->'targets') WHERE value->'reuse_reference'='false'::jsonb LOOP
    PERFORM w.id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=(target->'producer'->>'run_id')::uuid AND w.project_id=project AND w.policy_sha=snapshot_sha FOR SHARE;
    IF NOT FOUND THEN RETURN false;END IF;
    IF photo.lock_v27_native_handoff_qualification_chain(p_tenant,parent,(target->'producer'->>'run_id')::uuid,
     (SELECT (config_snapshot->'v27_target_defect_prepare'->>'person_run_id')::uuid FROM photo.runs WHERE tenant_id=p_tenant AND id=(target->'producer'->>'run_id')::uuid),project,r.created_by,session_id,snapshot_sha) IS NOT TRUE THEN RETURN false;END IF;
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
