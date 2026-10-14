-- +goose Up
-- Native person dispatch consumes existing immutable model_lock_sha pins.
-- No task/profile rewrite, new tenant table or new role privilege.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_dispatch_resources(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n photo.resource_nodes%ROWTYPE; t photo.tasks%ROWTYPE; hint photo.tasks%ROWTYPE; r photo.runs%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; op photo.operation_registry%ROWTYPE; params photo.artifacts%ROWTYPE;
 used_ram numeric; used_vram numeric; used_scratch numeric; used_gpu integer; quota_gpu integer; until_time timestamptz;
 envelope jsonb; model jsonb; backend text; model_lock text; input jsonb; ai photo.artifacts%ROWTYPE;
BEGIN
 IF p_task IS NULL OR p_event IS NULL OR p_correlation IS NULL OR p_node IS NULL OR p_trace IS NULL OR length(p_trace)>128
 OR p_ram IS NULL OR p_vram IS NULL OR p_scratch IS NULL OR p_ram<=0 OR p_vram<0 OR p_scratch<0 THEN
 RAISE EXCEPTION 'INVALID_RESERVATION' USING ERRCODE='22023'; END IF;
 SELECT * INTO hint FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::jsonb; RETURN; END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT n.enabled OR n.reported_at IS NULL OR n.reported_at<clock_timestamp()-interval '30 seconds' OR n.doctor_report_sha256 IS NULL THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE node_id=p_node AND resource_class=hint.resource_class AND task_id IS NULL ORDER BY slot_name LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_USAGE_MISSING'; END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE id=p_task FOR UPDATE;
 IF t.state<>'ready' OR t.attempt>=6 OR t.tenant_id<>hint.tenant_id OR t.run_id<>hint.run_id OR t.resource_class<>slot.resource_class THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 IF t.cancel_requested OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN
 RETURN QUERY SELECT 'cancelled',NULL::jsonb; RETURN; END IF;
 IF NOT r.graph_sealed THEN RETURN QUERY SELECT 'unsealed',NULL::jsonb; RETURN; END IF;
 SELECT * INTO op FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version;
 IF NOT FOUND OR op.resource_class<>t.resource_class THEN RAISE EXCEPTION 'OPERATION_MISMATCH' USING ERRCODE='22023'; END IF;
 backend:=t.execution_profile->>'backend';
 IF backend IS NULL OR NOT op.backends ? backend THEN RETURN QUERY SELECT 'unsupported_backend',NULL::jsonb; RETURN; END IF;
 model:=NULL;
 IF op.model_id IS NOT NULL THEN
  model_lock:=t.execution_profile->>'model_lock_id';
  -- Native person checkpoints pin model_lock_sha. Do not rewrite immutable
  -- task profiles or infer model identity for legacy/other operations.
  IF t.operation='mask.person' AND t.operation_version='1'
   AND t.execution_profile->>'renderer_version'='v2.2' THEN
   IF model_lock IS NOT NULL AND model_lock IS DISTINCT FROM t.execution_profile->>'model_lock_sha' THEN
    RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN;
   END IF;
   model_lock:=t.execution_profile->>'model_lock_sha';
  END IF;

  IF model_lock IS NULL OR model_lock !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN; END IF;
  model:=jsonb_build_object('id',op.model_id,'lock_id',model_lock);
 END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled
 AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class AND w.backends ? backend AND (w.qualified_operations->t.operation) ? backend
 AND w.last_seen>clock_timestamp()-interval '30 seconds' AND (op.model_id IS NULL OR w.lock_id=model_lock)) THEN
 RETURN QUERY SELECT 'waiting_worker',NULL::jsonb; RETURN; END IF;
 SELECT coalesce(sum(ram_bytes),0),coalesce(sum(vram_bytes),0),coalesce(sum(scratch_bytes),0),coalesce(sum(gpu_units),0)
 INTO used_ram,used_vram,used_scratch,used_gpu FROM photo.resource_reservations WHERE node_id=p_node AND state='reserved';
 IF used_ram+p_ram>least(n.ram_budget_bytes,n.ram_available_bytes) OR used_vram+p_vram>least(n.vram_budget_bytes,n.vram_available_bytes)
 OR used_scratch+p_scratch>least(n.scratch_budget_bytes,n.scratch_available_bytes) OR (t.resource_class='gpu' AND used_gpu>=1) THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT coalesce((quotas->>'max_active_gpu_tasks')::integer,1) INTO quota_gpu FROM photo.tenants WHERE id=t.tenant_id;
 IF t.resource_class='gpu' AND (SELECT active_gpu_tasks>=quota_gpu FROM photo.tenant_usage WHERE tenant_id=t.tenant_id) THEN
 RETURN QUERY SELECT 'waiting_quota',NULL::jsonb; RETURN; END IF;
 -- Artifact locks are acquired after task, in UUID order, including params.
 IF jsonb_typeof(t.inputs)<>'array' OR jsonb_array_length(t.inputs)>512 THEN RAISE EXCEPTION 'INVALID_INPUTS'; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(input)<>'object' OR (input-'artifact_id'-'sha256')<>'{}' OR input->>'artifact_id' IS NULL OR input->>'sha256' IS NULL OR input->>'sha256' !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INVALID_INPUTS' USING ERRCODE='22023'; END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.resource_reservations WHERE tenant_id=t.tenant_id AND task_id=t.id AND state='reserved') THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs || jsonb_build_array(jsonb_build_object('artifact_id',t.params_artifact_id))) ORDER BY value->>'artifact_id' LOOP
  SELECT * INTO ai FROM photo.artifacts WHERE tenant_id=t.tenant_id AND id=(input->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND OR ai.state<>'ready' OR (ai.expires_at IS NOT NULL AND ai.expires_at<=clock_timestamp()) OR
   (input ? 'sha256' AND ai.sha256 IS DISTINCT FROM input->>'sha256') THEN RETURN QUERY SELECT 'input_unavailable',NULL::jsonb; RETURN; END IF;
  IF ai.id=t.params_artifact_id THEN params:=ai; END IF;
 END LOOP;
 envelope:=jsonb_build_object('schema_version',1,'event_id',p_event,'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,
 'attempt',t.attempt+1,'operation',t.operation,'operation_version',t.operation_version,'resource_class',t.resource_class,'correlation_id',p_correlation,
 'inputs',t.inputs,'params',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256),'model',model,'timeout_seconds',op.timeout_seconds,'traceparent',p_trace);
 IF octet_length(envelope::text)>65536 THEN RAISE EXCEPTION 'COMMAND_TOO_LARGE' USING ERRCODE='22023'; END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,state,expires_at,fencing_token,gpu_units)
 VALUES(t.tenant_id,t.id,p_node,p_scratch,p_ram,p_vram,'reserved',until_time,t.fencing_token,CASE WHEN t.resource_class='gpu' THEN 1 ELSE 0 END)
 ON CONFLICT(tenant_id,task_id) DO UPDATE SET node_id=excluded.node_id,scratch_bytes=excluded.scratch_bytes,ram_bytes=excluded.ram_bytes,vram_bytes=excluded.vram_bytes,
 state='reserved',expires_at=excluded.expires_at,fencing_token=excluded.fencing_token,gpu_units=excluded.gpu_units;
 UPDATE photo.resource_slots SET task_id=t.id,tenant_id=t.tenant_id,fencing_token=t.fencing_token,lease_until=until_time WHERE id=slot.id;
 IF t.resource_class='gpu' THEN UPDATE photo.tenant_usage SET active_gpu_tasks=active_gpu_tasks+1 WHERE tenant_id=t.tenant_id; END IF;
 UPDATE photo.tasks SET state='queued',dispatch_command=envelope,timeout_seconds=op.timeout_seconds,terminal_reason=NULL WHERE id=t.id;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,p_event,'photo.commands.v1',op.routing_key,envelope);
 RETURN QUERY SELECT 'queued',envelope;
END $$;

ALTER FUNCTION photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE claimed record; task_row photo.tasks%ROWTYPE; run_row photo.runs%ROWTYPE;
BEGIN
 -- Preserve source/profile bytes while requiring the exact pinned LRASPP
 -- worker identity even when historical model_lock_id is absent.
 task_row:=photo.lock_task(p_task);
 IF task_row.operation='mask.person' AND task_row.operation_version='1'
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
  IF run_row.kind='apply' AND run_row.config_snapshot ? 'approved_semantic' THEN
   IF NOT run_row.graph_sealed OR run_row.cancel_requested OR run_row.status NOT IN('queued','running') THEN
    RAISE EXCEPTION 'SEMANTIC_APPLY_START_STALE' USING ERRCODE='40001';END IF;
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

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
