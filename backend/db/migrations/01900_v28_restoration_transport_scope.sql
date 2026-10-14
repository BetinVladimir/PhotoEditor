-- +goose Up
-- Scope input transport to the exact retained restoration task and live editor
-- session. The descriptor reader independently replays mask origin and policy.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_transport_scope(p_tenant uuid,p_run uuid,
 p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;plan jsonb;task jsonb;body text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_purpose NOT IN('v2_input_chunk_0','v2_input_manifest') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
 AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR r.v28_restoration_descriptor_jcs IS NULL
 OR r.v28_restoration_session_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF NOT FOUND OR t.operation NOT IN('portrait.glare','image.reflection')
 OR t.operation_version IS DISTINCT FROM '1' OR t.resource_class IS DISTINCT FROM 'gpu'
 OR t.state IS DISTINCT FROM 'blocked' OR t.attempt IS DISTINCT FROM 0
 OR t.cancel_requested IS DISTINCT FROM false THEN RETURN false;END IF;
 body:=photo.read_v28_restoration_descriptor(p_tenant,p_run,r.created_by,r.v28_restoration_session_id);
 IF body IS NULL OR body IS DISTINCT FROM r.v28_restoration_descriptor_jcs THEN RETURN false;END IF;
 plan:=body::jsonb;task:=plan->'task';
 IF task->>'id' IS DISTINCT FROM t.id::text OR task->>'node_key' IS DISTINCT FROM t.node_key
 OR task->>'operation' IS DISTINCT FROM t.operation
 OR task->>'params_artifact_id' IS DISTINCT FROM t.params_artifact_id::text
 OR task->>'input_digest' IS DISTINCT FROM t.input_digest
 OR (t.execution_profile IS DISTINCT FROM task->'execution_profile' AND NOT
  (photo.v2_input_closure_ready(t.id) AND
   (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=
   task->'execution_profile'))
 OR photo.v28_restoration_inventory_digest_ready(t.id,t.params_artifact_id,
  task->>'params_sha',t.operation,t.input_digest,plan->'entries',plan->'params',
  task->'execution_profile'->'restoration_qualification') IS DISTINCT FROM true
 THEN RETURN false;END IF;
 PERFORM d.artifact_id FROM photo.backend_documents d JOIN photo.artifacts a
 ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.purpose='task_params'
 AND d.state='ready' AND d.artifact_id=t.params_artifact_id
 AND d.sha256=task->>'params_sha'
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant
 AND holder_type='run' AND holder_id=p_run AND state='reserved'
 AND expires_at>clock_timestamp() AND bytes>=2162688 FOR SHARE;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_transport_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_transport_scope(uuid,uuid,uuid,text) FROM PUBLIC;

-- +goose StatementBegin
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text)
 RENAME TO v2_input_document_scope_pre_v28_restoration;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope_pre_v28_restoration(uuid,uuid,uuid,text) FROM PUBLIC;
CREATE FUNCTION photo.v2_input_document_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
  AND v28_restoration_descriptor_jcs IS NOT NULL)
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=p_task AND operation IN('portrait.glare','image.reflection')) THEN
  RETURN photo.v28_restoration_transport_scope(p_tenant,p_run,p_task,p_purpose);END IF;
 RETURN photo.v2_input_document_scope_pre_v28_restoration(p_tenant,p_run,p_task,p_purpose);
END $$;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb)
 RENAME TO register_v2_task_input_batch_pre_v28_restoration;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch_pre_v28_restoration(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.register_v2_task_input_batch(p_tenant uuid,p_run uuid,p_task uuid,
 p_first integer,p_rows jsonb)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;expected jsonb;size integer;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.v28_restoration_descriptor_jcs IS NOT NULL OR t.operation IN('portrait.glare','image.reflection') THEN
  IF p_first IS NULL OR p_first NOT BETWEEN 0 AND 126
   OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array'
   OR jsonb_array_length(p_rows) NOT BETWEEN 1 AND 127
   OR p_first+jsonb_array_length(p_rows)>127
   OR photo.v28_restoration_transport_scope(p_tenant,p_run,p_task,'v2_input_manifest') IS DISTINCT FROM true
   THEN RAISE EXCEPTION 'V28_RESTORATION_INPUT_BATCH_UNAVAILABLE' USING ERRCODE='23514';END IF;
  size:=jsonb_array_length(p_rows);
  SELECT jsonb_agg(value ORDER BY ordinal) INTO expected
   FROM jsonb_array_elements((r.v28_restoration_descriptor_jcs::jsonb)->'entries')
    WITH ORDINALITY entries(value,ordinal)
   WHERE ordinal>p_first AND ordinal<=p_first+size;
  IF expected IS DISTINCT FROM p_rows THEN
   RAISE EXCEPTION 'V28_RESTORATION_INPUT_BATCH_CHANGED' USING ERRCODE='23514';END IF;
 END IF;
 RETURN photo.register_v2_task_input_batch_pre_v28_restoration(p_tenant,p_run,p_task,p_first,p_rows);
END $$;
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_registered_inventory_ready(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT COALESCE(EXISTS(SELECT 1 FROM photo.runs r JOIN photo.tasks t
  ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND t.id=p_task
  AND photo.v28_restoration_transport_scope(p_tenant,p_run,p_task,'v2_input_manifest') IS TRUE
  AND (r.v28_restoration_descriptor_jcs::jsonb)->'entries'=(SELECT jsonb_agg(
   jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
    'contract',i.contract) ORDER BY i.ordinal)
   FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.contract NOT IN('InputClosureChunk','InputClosureManifest'))),false);
$$;
ALTER FUNCTION photo.v28_restoration_registered_inventory_ready(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_registered_inventory_ready(uuid,uuid,uuid) FROM PUBLIC;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb)
 RENAME TO seal_v2_task_inputs_pre_v28_restoration;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs_pre_v28_restoration(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_task_inputs(p_tenant uuid,p_run uuid,p_task uuid,
 p_manifest jsonb,p_chunks jsonb,p_entry_count integer,p_entries_sha text,p_direct_roots jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;entry jsonb;roots jsonb;
 body text:='[';separator text:='';
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.v28_restoration_descriptor_jcs IS NOT NULL OR t.operation IN('portrait.glare','image.reflection') THEN
  IF photo.v28_restoration_registered_inventory_ready(p_tenant,p_run,p_task) IS DISTINCT FROM true
   OR p_entry_count IS DISTINCT FROM jsonb_array_length((r.v28_restoration_descriptor_jcs::jsonb)->'entries')
   OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_chunks)<>1
   THEN RETURN false;END IF;
  SELECT jsonb_agg(value->'ref' ORDER BY ordinal) INTO roots
   FROM jsonb_array_elements((r.v28_restoration_descriptor_jcs::jsonb)->'entries')
    WITH ORDINALITY entries(value,ordinal);
  IF p_direct_roots IS DISTINCT FROM roots THEN RETURN false;END IF;
  FOR entry IN SELECT value FROM jsonb_array_elements((r.v28_restoration_descriptor_jcs::jsonb)->'entries') LOOP
   body:=body||separator||'{"contract":"'||(entry->>'contract')||
    '","ref":{"artifact_id":"'||(entry->'ref'->>'artifact_id')||
    '","sha256":"'||(entry->'ref'->>'sha256')||'"}}';separator:=',';
  END LOOP;
  body:=body||']';
  IF p_entries_sha IS DISTINCT FROM encode(sha256(convert_to(body,'UTF8')),'hex') THEN RETURN false;END IF;
 END IF;
 RETURN photo.seal_v2_task_inputs_pre_v28_restoration(p_tenant,p_run,p_task,
  p_manifest,p_chunks,p_entry_count,p_entries_sha,p_direct_roots);
END $$;
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
