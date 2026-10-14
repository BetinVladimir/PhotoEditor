-- +goose Up
-- Automatic completion is a durable parent handoff, not face-stage readiness.
-- No data rewrite, new application DML grant, object IO or capability activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_native_automatic_handoff_ready(p_tenant uuid,p_detector uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;p photo.runs%ROWTYPE;d photo.runs%ROWTYPE;
 project uuid;session_id uuid;parent uuid;native jsonb;person_task uuid;
 proof photo.semantic_native_protections%ROWTYPE;node jsonb;workflow uuid;pin jsonb;
BEGIN
 -- Called under the detector recovery lease lock by the recording boundary.
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_detector;
 IF NOT FOUND OR r.v27_initial_manual_selection IS NOT NULL THEN RETURN false;END IF;
 native:=r.config_snapshot->'v27_detector_prepare';
 parent:=(native->>'parent_run_id')::uuid;
 project:=(r.config_snapshot->>'project_id')::uuid;
 session_id:=(native->>'auth_session_id')::uuid;
 IF parent IS NULL OR project IS NULL OR session_id IS NULL
 OR p_detector IS DISTINCT FROM photo.semantic_uuid_v5(parent,'semantic.face.detect:reference') THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF photo.v2_editor_authorized(p_tenant,project,r.created_by,session_id) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO p FROM photo.runs WHERE tenant_id=p_tenant AND id=parent FOR SHARE;
 IF NOT FOUND OR p.kind<>'v2' OR NOT p.graph_sealed
 OR p.created_by IS DISTINCT FROM r.created_by
 OR p.config_snapshot->>'project_id' IS DISTINCT FROM project::text
 OR p.config_snapshot->'v27_semantic'->>'auth_session_id' IS DISTINCT FROM session_id::text
 OR p.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR jsonb_typeof(p.config_snapshot->'v27_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT id INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=parent AND project_id=project;
 IF workflow IS NULL OR NOT EXISTS(SELECT 1 FROM photo.idempotency_records i
 WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=r.created_by
 AND i.route='V2.Admit' AND i.resource_id=parent AND i.request_hash=native->>'request_sha'
 AND i.response_status=202 AND i.response_body=jsonb_build_object('run_id',parent,'workflow_id',workflow,'state','queued')) THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5(parent,'semantic.defect.prepare:reference') FOR SHARE;
 IF NOT FOUND OR NOT d.graph_sealed OR d.config_snapshot->'v27_defect_native_prepare'->>'request_sha' IS DISTINCT FROM native->>'request_sha'
 OR d.config_snapshot->'v27_defect_native_prepare'->'source' IS DISTINCT FROM native->'source' THEN RETURN false;END IF;
 SELECT saved.task_id INTO person_task FROM photo.semantic_native_input_saved_stage(p_tenant,project,r.created_by,session_id,parent,
 photo.semantic_uuid_v5(parent,'semantic.mask.person:reference'),'mask.person') saved;
 IF person_task IS NULL THEN RETURN false;END IF;
 PERFORM t.id FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
 AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 WHERE t.tenant_id=p_tenant AND t.id=person_task AND t.state='succeeded' FOR SHARE OF t,z;
 IF NOT FOUND THEN RETURN false;END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
 d.config_snapshot->'v27_defect_native_prepare'->'person_input'->'root',
 d.config_snapshot->'v27_defect_native_prepare'->'person_input'->'pixels')) LOOP
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id::text=pin->>'artifact_id' AND sha256=pin->>'sha256'
  AND state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF photo.semantic_native_original_person_result(p_tenant,project,r.created_by,session_id,parent,native->>'request_sha',person_task,
 (d.config_snapshot->'v27_defect_native_prepare'->'person_input'->'root'->>'artifact_id')::uuid,
 d.config_snapshot->'v27_defect_native_prepare'->'person_input'->'root'->>'sha256',
 (d.config_snapshot->'v27_defect_native_prepare'->'person_input'->'pixels'->>'artifact_id')::uuid,
 d.config_snapshot->'v27_defect_native_prepare'->'person_input'->'pixels'->>'sha256') IS NOT TRUE THEN RETURN false;END IF;
 -- Pin the original canonical producer/result; no source-wide alternate mask.
 SELECT protection.* INTO proof FROM photo.semantic_native_protections protection
 JOIN photo.tasks t ON t.tenant_id=protection.tenant_id AND t.id=protection.task_id AND t.run_id=d.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.id=protection.result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 WHERE protection.tenant_id=p_tenant AND protection.project_id=project AND protection.parent_run_id=parent
 AND t.id::text=d.config_snapshot->'v27_defect_native_prepare'->>'task_id'
 AND t.state='succeeded' AND t.operation='defects.propose' AND t.operation_version='1'
 AND protection.asset_version_id::text=native->'source'->>'asset_version_id'
 AND protection.source_id::text=native->'source'->'source'->>'artifact_id'
 AND protection.source_sha=native->'source'->'source'->>'sha256'
 FOR SHARE OF protection,t,z;
 IF NOT FOUND THEN RETURN false;END IF;
 FOR pin IN SELECT value FROM jsonb_array_elements(jsonb_build_array(
 jsonb_build_object('artifact_id',proof.root_id,'sha256',proof.root_sha),
 jsonb_build_object('artifact_id',proof.pixels_id,'sha256',proof.pixels_sha),
 jsonb_build_object('artifact_id',proof.report_id,'sha256',proof.report_sha))) LOOP
  PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id::text=pin->>'artifact_id' AND sha256=pin->>'sha256'
  AND state='ready' AND verified_at IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN('','null')
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 IF jsonb_array_length(p.config_snapshot->'v27_semantic'->'nodes') NOT BETWEEN 1 AND 5000
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=parent)
 <>jsonb_array_length(p.config_snapshot->'v27_semantic'->'nodes') THEN RETURN false;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p.config_snapshot->'v27_semantic'->'nodes') LOOP
  IF node->'source_protection' IS DISTINCT FROM jsonb_build_object('artifact_id',proof.root_id,'sha256',proof.root_sha)
  OR NOT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
  WHERE t.tenant_id=p_tenant AND t.run_id=parent AND t.id::text=node->>'task_id'
  AND t.operation='semantic.transfer' AND t.operation_version='1'
  AND i.contract='EditIntent' AND i.artifact_id::text=native->'intent'->>'artifact_id' AND i.sha256=native->'intent'->>'sha256') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_native_automatic_handoff_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_automatic_handoff_ready(uuid,uuid) FROM PUBLIC;
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
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
