-- +goose Up
-- Target defect exact claim/start and running-only native output guard.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record; task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE; stage_name text; previous_tenant text; target_ready boolean;
BEGIN
 -- Preserve source/profile bytes while requiring the exact pinned native
 -- GPU model identity. Identity-free historical profiles remain unavailable.
 task_row:=photo.lock_task(p_task);
 IF task_row.operation IN('mask.person','face.detect','face.landmarks','face.parse') AND task_row.operation_version='1'
  AND task_row.execution_profile->>'renderer_version'='v2.2'
  AND (task_row.execution_profile->>'model_lock_sha' IS NULL
   OR task_row.execution_profile->>'model_lock_sha' !~ '^[0-9a-f]{64}$'
   OR (task_row.execution_profile ? 'model_lock_id' AND task_row.execution_profile->>'model_lock_id' IS DISTINCT FROM task_row.execution_profile->>'model_lock_sha')
   OR p_command->'model'->>'lock_id' IS DISTINCT FROM task_row.execution_profile->>'model_lock_sha'
   OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=task_row.tenant_id AND w.worker_id=p_worker
     AND w.lock_id=task_row.execution_profile->>'model_lock_sha')) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 SELECT * INTO claimed FROM photo.claim_task_before_semantic_start(p_task,p_attempt,p_worker,p_command);
 IF claimed.outcome='claimed' THEN
  SELECT * INTO task_row FROM photo.tasks WHERE id=p_task;
  SELECT * INTO run_row FROM photo.runs WHERE tenant_id=task_row.tenant_id AND id=task_row.run_id FOR UPDATE;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_defect_prepare' THEN
   IF task_row.operation IS DISTINCT FROM 'defects.propose' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running')
    OR run_row.config_snapshot->'v27_defect_prepare'->>'task_id' IS DISTINCT FROM task_row.id::text
    OR run_row.config_snapshot->'v27_defect_prepare'->>'input_digest' IS DISTINCT FROM task_row.input_digest
    OR NOT photo.v2_input_closure_ready(task_row.id)
    OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=run_row.tenant_id AND run_id=run_row.id AND workflow_type='semantic' AND renderer_version='v2.2') THEN
    RAISE EXCEPTION 'SEMANTIC_DEFECT_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  stage_name:=CASE task_row.operation
   WHEN 'face.detect' THEN 'v27_detector_prepare'
   WHEN 'face.landmarks' THEN 'v27_face_prepare'
   WHEN 'face.parse' THEN 'v27_face_prepare'
   WHEN 'mask.face_skin' THEN 'v27_skin_prepare'
   WHEN 'defects.propose' THEN 'v27_defect_native_prepare'
   ELSE NULL END;
  IF run_row.kind='v2' AND stage_name IS NOT NULL AND run_row.config_snapshot ? stage_name THEN
   IF task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running')
    OR run_row.config_snapshot->stage_name->>'task_id' IS DISTINCT FROM task_row.id::text
    OR run_row.config_snapshot->stage_name->>'input_digest' IS DISTINCT FROM task_row.input_digest
    OR (stage_name<>'v27_detector_prepare' AND run_row.config_snapshot->stage_name->>'operation' IS DISTINCT FROM task_row.operation)
    OR NOT photo.v2_input_closure_ready(task_row.id)
    OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=run_row.tenant_id AND run_id=run_row.id AND workflow_type='semantic' AND renderer_version='v2.2') THEN
    RAISE EXCEPTION '%',CASE WHEN stage_name='v27_skin_prepare' THEN 'SEMANTIC_SKIN_START_STALE' WHEN stage_name='v27_defect_native_prepare' THEN 'SEMANTIC_NATIVE_DEFECT_START_STALE' ELSE 'SEMANTIC_FACE_START_STALE' END USING ERRCODE='40001'; END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514'; END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_person_prepare' THEN
   IF task_row.operation IS DISTINCT FROM 'mask.person' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running')
    OR run_row.config_snapshot->'v27_person_prepare'->>'task_id' IS DISTINCT FROM task_row.id::text
    OR run_row.config_snapshot->'v27_person_prepare'->>'input_digest' IS DISTINCT FROM task_row.input_digest
    OR NOT photo.v2_input_closure_ready(task_row.id)
    OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=run_row.tenant_id AND run_id=run_row.id AND workflow_type='semantic' AND renderer_version='v2.2') THEN
    RAISE EXCEPTION 'SEMANTIC_PERSON_START_STALE' USING ERRCODE='40001'; END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514'; END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_target_person_prepare' THEN
   IF task_row.operation IS DISTINCT FROM 'mask.person' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running')
    OR run_row.config_snapshot->'v27_target_person_prepare'->>'task_id' IS DISTINCT FROM task_row.id::text
    OR run_row.config_snapshot->'v27_target_person_prepare'->>'input_digest' IS DISTINCT FROM task_row.input_digest
    OR run_row.id IS DISTINCT FROM photo.semantic_uuid_v5((run_row.config_snapshot->'v27_target_person_prepare'->>'target_namespace')::uuid,'semantic.mask.person:reference')
    OR task_row.id IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(run_row.id,'workflow'),'mask.person:0')
    OR task_row.node_key IS DISTINCT FROM 'mask.person:0'
    OR coalesce(task_row.execution_profile->>'runtime_lock_sha','') !~ '^[0-9a-f]{64}$'
    OR coalesce(task_row.execution_profile->>'device_profile_sha','') !~ '^[0-9a-f]{64}$'
    OR NOT photo.v2_input_closure_ready(task_row.id)
    OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=run_row.tenant_id AND run_id=run_row.id AND workflow_type='semantic' AND renderer_version='v2.2') THEN
    RAISE EXCEPTION 'SEMANTIC_TARGET_PERSON_START_STALE' USING ERRCODE='40001'; END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514'; END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_target_detector_prepare' THEN
   IF task_row.operation IS DISTINCT FROM 'face.detect' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running')
    OR run_row.config_snapshot->'v27_target_detector_prepare'->>'task_id' IS DISTINCT FROM task_row.id::text
    OR run_row.config_snapshot->'v27_target_detector_prepare'->>'input_digest' IS DISTINCT FROM task_row.input_digest
    OR run_row.id IS DISTINCT FROM photo.semantic_uuid_v5((run_row.config_snapshot->'v27_target_detector_prepare'->>'target_namespace')::uuid,'semantic.face.detect:reference')
    OR task_row.id IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(run_row.id,'workflow'),'face.detect:0')
    OR task_row.node_key IS DISTINCT FROM 'face.detect:0'
    OR coalesce(task_row.execution_profile->>'runtime_lock_sha','') !~ '^[0-9a-f]{64}$'
    OR coalesce(task_row.execution_profile->>'device_profile_sha','') !~ '^[0-9a-f]{64}$'
    OR NOT photo.v2_input_closure_ready(task_row.id)
    OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=run_row.tenant_id AND run_id=run_row.id AND workflow_type='semantic' AND renderer_version='v2.2') THEN
    RAISE EXCEPTION 'SEMANTIC_TARGET_DETECTOR_START_STALE' USING ERRCODE='40001'; END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514'; END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='apply' AND run_row.config_snapshot ? 'approved_semantic' THEN
   IF NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running') THEN
    RAISE EXCEPTION 'SEMANTIC_APPLY_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_target_face_prepare' THEN
   previous_tenant:=current_setting('app.tenant_id',true);
   PERFORM set_config('app.tenant_id',task_row.tenant_id::text,true);
   target_ready:=photo.lock_v27_target_face_sealed_graph_scope(task_row.tenant_id,run_row.id,task_row.id);
   PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
   IF task_row.operation NOT IN('face.landmarks','face.parse') OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR target_ready IS NOT TRUE
    OR NOT photo.v2_input_closure_ready(task_row.id) THEN
    RAISE EXCEPTION 'SEMANTIC_TARGET_FACE_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_target_skin_prepare' THEN
   previous_tenant:=current_setting('app.tenant_id',true);
   PERFORM set_config('app.tenant_id',task_row.tenant_id::text,true);
   target_ready:=photo.lock_v27_target_skin_sealed_graph_scope(task_row.tenant_id,run_row.id,task_row.id);
   PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
   IF task_row.operation IS DISTINCT FROM 'mask.face_skin' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR target_ready IS NOT TRUE
    OR NOT photo.v2_input_closure_ready(task_row.id) THEN
    RAISE EXCEPTION 'SEMANTIC_TARGET_SKIN_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;  IF run_row.kind='v2' AND run_row.config_snapshot ? 'v27_target_defect_prepare' THEN
   previous_tenant:=current_setting('app.tenant_id',true);
   PERFORM set_config('app.tenant_id',task_row.tenant_id::text,true);
   target_ready:=photo.lock_v27_target_defect_sealed_graph_scope(task_row.tenant_id,run_row.id,task_row.id);
   PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
   IF task_row.operation IS DISTINCT FROM 'defects.propose' OR task_row.operation_version IS DISTINCT FROM '1'
    OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR target_ready IS NOT TRUE
    OR NOT photo.v2_input_closure_ready(task_row.id) THEN
    RAISE EXCEPTION 'SEMANTIC_TARGET_DEFECT_START_STALE' USING ERRCODE='40001';END IF;
   IF run_row.status='queued' THEN
    IF run_row.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
    UPDATE photo.runs SET status='running',revision=revision+1 WHERE tenant_id=run_row.tenant_id AND id=run_row.id;
   END IF;
  END IF;
 END IF;
 RETURN QUERY SELECT claimed.outcome,claimed.fence,claimed.lease_until,claimed.deadline,claimed.context;
END $$;
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;

CREATE FUNCTION photo.semantic_target_defect_result_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_outputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;c jsonb;ref jsonb;seen uuid[]:='{}';aid uuid;i integer;roles text[];media text[];
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT FOUND OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' OR t.cancel_requested
 OR p_attempt IS DISTINCT FROM t.attempt OR p_fence IS DISTINCT FROM t.fencing_token OR p_attempt<1 OR p_fence<1 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF NOT FOUND OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status<>'running' THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_target_defect_prepare';
 IF t.operation IS DISTINCT FROM 'defects.propose' THEN RETURN false;END IF;
 roles:=ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report'];media:=ARRAY['application/octet-stream','application/octet-stream','image/png','application/json','application/json'];
 IF c->>'operation' IS DISTINCT FROM t.operation
 OR r.id IS DISTINCT FROM photo.semantic_uuid_v5((c->>'target_namespace')::uuid,'semantic.defect.prepare:reference')
 OR r.config_snapshot ? 'v27_defect_prepare' OR r.config_snapshot ? 'v27_skin_prepare'
 OR r.config_snapshot ? 'v27_face_prepare' OR r.config_snapshot ? 'v27_detector_prepare' OR r.config_snapshot ? 'v27_person_prepare' THEN RETURN false;END IF;
 IF t.id IS DISTINCT FROM photo.semantic_uuid_v5(photo.semantic_uuid_v5(r.id,'workflow'),'defects.propose:0') OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 IF c->'schema_version' IS DISTINCT FROM '1'::jsonb OR c->>'task_id' IS DISTINCT FROM t.id::text OR c->>'input_digest' IS DISTINCT FROM t.input_digest
 OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id AND project_id=(r.config_snapshot->>'project_id')::uuid AND renderer_version='v2.2' AND workflow_type='semantic')
 OR jsonb_typeof(p_outputs) IS DISTINCT FROM 'array' OR jsonb_array_length(p_outputs)<>cardinality(roles) OR octet_length(p_outputs::text)>8192 THEN RETURN false;END IF;
 FOR i IN 0..cardinality(roles)-1 LOOP
  ref:=p_outputs->i;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid IS NULL OR aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(seen) OR aid=t.params_artifact_id THEN RETURN false;END IF;
  seen:=array_append(seen,aid);
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
  WHERE a.tenant_id=p_tenant AND a.id=aid AND a.sha256=ref->>'sha256' AND a.output_role=roles[i+1] AND a.media_type=media[i+1]
  AND a.bytes>0 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.created_by_task_id=t.id AND a.produced_attempt=p_attempt AND a.produced_fence=p_fence
  AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready' AND u.completed_at IS NOT NULL
  AND u.role=a.output_role AND u.bytes=a.bytes AND u.sha256=a.sha256 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version AND u.media_type=a.media_type) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_target_defect_result_ready(uuid,uuid,integer,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_target_defect_result_ready(uuid,uuid,integer,bigint,jsonb) FROM PUBLIC;
CREATE OR REPLACE FUNCTION photo.guard_v2_result_root() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE; root jsonb;
BEGIN
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=NEW.tenant_id AND id=NEW.task_id;
 IF NOT FOUND THEN RETURN NEW; END IF;
 SELECT * INTO run_row FROM photo.runs WHERE tenant_id=task_row.tenant_id AND id=task_row.run_id;
 IF run_row.kind<>'v2' THEN RETURN NEW; END IF;
 IF task_row.operation='defects.propose' AND run_row.config_snapshot ? 'v27_target_defect_prepare' THEN
  IF photo.semantic_target_defect_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_TARGET_DEFECT_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation='mask.face_skin' AND run_row.config_snapshot ? 'v27_target_skin_prepare' THEN
  IF photo.semantic_target_skin_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_TARGET_SKIN_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation IN('face.landmarks','face.parse') AND run_row.config_snapshot ? 'v27_target_face_prepare' THEN
  IF photo.semantic_target_face_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_TARGET_FACE_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation='face.detect' AND run_row.config_snapshot ? 'v27_target_detector_prepare' THEN
  IF photo.semantic_target_detector_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_TARGET_DETECTOR_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation='mask.person' AND run_row.config_snapshot ? 'v27_target_person_prepare' THEN
  IF photo.semantic_target_person_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_TARGET_PERSON_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation='defects.propose' AND run_row.config_snapshot ? 'v27_defect_native_prepare' THEN
  IF photo.semantic_native_defect_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_NATIVE_DEFECT_INPUT_CHAIN_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation='mask.face_skin' AND run_row.config_snapshot ? 'v27_skin_prepare' THEN
  IF photo.semantic_native_skin_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_NATIVE_SKIN_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation IN ('face.landmarks','face.parse') AND run_row.config_snapshot ? 'v27_face_prepare' THEN
  IF photo.semantic_native_face_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_NATIVE_FACE_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (task_row.operation='face.detect' AND run_row.config_snapshot ? 'v27_detector_prepare')
 OR (task_row.operation='mask.person' AND run_row.config_snapshot ? 'v27_person_prepare') THEN
  IF photo.semantic_native_preparation_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_NATIVE_PREPARATION_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF task_row.operation='defects.propose' AND run_row.config_snapshot ? 'v27_defect_prepare' THEN
  IF photo.semantic_defect_native_result_ready(NEW.tenant_id,NEW.task_id,NEW.attempt,NEW.fencing_token,NEW.outputs) IS NOT TRUE THEN
   RAISE EXCEPTION 'V27_NATIVE_DEFECT_OUTPUTS_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=run_row.tenant_id
   AND w.run_id=run_row.id AND w.renderer_version='v2.2') THEN
  RAISE EXCEPTION 'V2_RENDERER_MISMATCH' USING ERRCODE='23514';
 END IF;
 IF jsonb_typeof(NEW.outputs)<>'array' OR jsonb_array_length(NEW.outputs)<>1 THEN
  RAISE EXCEPTION 'V2_ROOT_REQUIRED' USING ERRCODE='23514';
 END IF;
 root:=NEW.outputs->0;
 IF jsonb_typeof(root)<>'object' OR (root-'artifact_id'-'sha256')<>'{}'::jsonb
  OR root->>'artifact_id' IS NULL OR root->>'sha256' IS NULL
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a
   JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id
   WHERE a.tenant_id=NEW.tenant_id AND a.id=(root->>'artifact_id')::uuid
   AND a.sha256=root->>'sha256' AND a.output_role='document' AND a.state='ready'
   AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
   AND a.created_by_task_id=task_row.id AND a.produced_attempt=NEW.attempt
   AND a.produced_fence=NEW.fencing_token AND u.state='ready' AND u.role='document'
   AND u.task_id=task_row.id AND u.attempt=NEW.attempt AND u.fence=NEW.fencing_token) THEN
  RAISE EXCEPTION 'V2_ROOT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;


-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
