-- +goose Up
-- Automatic completion is a durable parent handoff, not face-stage readiness.
-- No data rewrite, new application DML grant, object IO or capability activation.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_native_automatic_handoff_ready(p_tenant uuid,p_detector uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;p photo.runs%ROWTYPE;d photo.runs%ROWTYPE;
 project uuid;session_id uuid;parent uuid;native jsonb;person_task uuid;
 proof photo.semantic_native_protections%ROWTYPE;node jsonb;workflow uuid;pin jsonb;
 target jsonb;marker jsonb;binding jsonb;resolved record;canonical record;idx integer:=0;
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
 -- All-native targets remain live dependencies at handoff, not only at seal.
 IF p.config_snapshot ? 'v27_native_targets_transfer' THEN
  marker:=p.config_snapshot->'v27_native_targets_transfer';
  IF marker->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR marker->>'parent_run_id' IS DISTINCT FROM parent::text
  OR marker->>'request_sha' IS DISTINCT FROM native->>'request_sha'
  OR jsonb_typeof(marker->'targets') IS DISTINCT FROM 'array'
  OR jsonb_array_length(marker->'targets')<>jsonb_array_length(p.config_snapshot->'v27_semantic'->'nodes')
  OR p.cancel_requested OR p.status NOT IN('queued','running','waiting_review','completed') THEN RETURN false;END IF;
  FOR target IN SELECT value FROM jsonb_array_elements(marker->'targets') LOOP
   binding:=target->'source';node:=p.config_snapshot->'v27_semantic'->'nodes'->idx;
   IF target->'ordinal' IS DISTINCT FROM to_jsonb(idx)
   OR node->'target_protection' IS DISTINCT FROM target->'protection'->'ref'
   OR NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=parent
    AND id::text=node->>'task_id' AND input_digest=target->>'input_digest' AND NOT cancel_requested) THEN RETURN false;END IF;
   PERFORM a.id FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id
    WHERE i.tenant_id=p_tenant AND i.task_id=(node->>'task_id')::uuid ORDER BY a.id FOR SHARE OF a;
   IF photo.v2_input_closure_ready((node->>'task_id')::uuid) IS NOT TRUE THEN RETURN false;END IF;
   PERFORM a.id FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id
    WHERE a.tenant_id=p_tenant AND a.project_id=project AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND v.id::text=binding->>'asset_version_id' AND v.review_state='accepted'
    AND v.master_artifact_id::text=binding->'source'->>'artifact_id' AND v.content_hash=binding->'source'->>'sha256'
    FOR SHARE OF a,v;
   IF NOT FOUND THEN RETURN false;END IF;
   PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id::text=target->'protection'->'ref'->>'artifact_id'
    AND sha256=target->'protection'->'ref'->>'sha256' AND bucket=target->'protection'->>'bucket'
    AND object_key=target->'protection'->>'key' AND object_version=target->'protection'->>'version'
    AND bytes=(target->'protection'->>'bytes')::bigint AND media_type=target->'protection'->>'media'
    AND state='ready' AND verified_at IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
   IF NOT FOUND THEN RETURN false;END IF;
   IF target->'reuse_reference'='true'::jsonb THEN
    IF target->'producer' IS DISTINCT FROM 'null'::jsonb OR binding IS DISTINCT FROM marker->'reference'
     OR target->'protection' IS DISTINCT FROM marker->'reference_protection'
     OR target->'pixels' IS DISTINCT FROM marker->'reference_pixels' THEN RETURN false;END IF;
   ELSE
    IF target->'reuse_reference' IS DISTINCT FROM 'false'::jsonb THEN RETURN false;END IF;
    PERFORM producer.id FROM photo.runs producer JOIN photo.tasks t ON t.tenant_id=producer.tenant_id AND t.run_id=producer.id
     JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
      AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
     WHERE producer.tenant_id=p_tenant AND producer.id::text=target->'producer'->>'run_id'
     AND t.id::text=target->'producer'->>'task_id' AND z.id::text=target->'producer'->>'result_id'
     AND producer.graph_sealed AND producer.status='completed' AND NOT producer.cancel_requested
     AND NOT t.cancel_requested AND t.state='succeeded'
     AND producer.config_snapshot->'v27_target_defect_prepare'->'source'=binding
     AND producer.config_snapshot->'v27_target_defect_prepare'->>'target_namespace'=target->>'namespace'
     FOR SHARE OF producer,t,z;
    IF NOT FOUND THEN RETURN false;END IF;
    PERFORM document_id FROM photo.semantic_native_protections WHERE tenant_id=p_tenant
     AND task_id=(target->'producer'->>'task_id')::uuid ORDER BY document_id FOR SHARE;
    SELECT * INTO canonical FROM photo.semantic_target_defect_result_catalog(p_tenant,project,r.created_by,session_id,parent,
     native->>'request_sha',(target->'producer'->>'task_id')::uuid);
    IF NOT FOUND OR canonical.result_id::text IS DISTINCT FROM target->'producer'->>'result_id'
     OR canonical.params IS DISTINCT FROM target->'producer'->'params'
     OR canonical.outputs->1 IS DISTINCT FROM target->'pixels' THEN RETURN false;END IF;
    SELECT * INTO resolved FROM photo.semantic_target_defect_protection_object(p_tenant,project,r.created_by,session_id,parent,
     native->>'request_sha',(target->'producer'->>'task_id')::uuid);
    IF NOT FOUND OR target->'protection' IS DISTINCT FROM jsonb_build_object('role','protection_root',
     'ref',jsonb_build_object('artifact_id',resolved.artifact_id,'sha256',resolved.sha256),'bucket',resolved.bucket,
     'key',resolved.object_key,'version',resolved.object_version,'bytes',resolved.bytes,'media',resolved.media_type) THEN RETURN false;END IF;
   END IF;
   idx:=idx+1;
  END LOOP;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_native_automatic_handoff_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_native_automatic_handoff_ready(uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
