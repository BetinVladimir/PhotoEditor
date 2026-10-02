-- The compiler calls batches and seal in one DB transaction after immutable
-- transport objects have been published. No worker or API role can write rows.
-- +goose StatementBegin
CREATE FUNCTION photo.register_v2_task_input_batch(
 p_tenant uuid,p_run uuid,p_task uuid,p_first integer,p_rows jsonb)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; project uuid;
 next_ordinal integer; prior uuid; item jsonb; ref jsonb; aid uuid; ordinal integer:=p_first;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_task IS NULL OR p_first IS NULL OR p_first NOT BETWEEN 0 AND 999999
  OR jsonb_typeof(p_rows) IS DISTINCT FROM 'array' OR jsonb_array_length(p_rows) NOT BETWEEN 1 AND 512
  OR pg_column_size(p_rows)>1048576 OR p_first+jsonb_array_length(p_rows)>1000000 THEN
  RAISE EXCEPTION 'V2_INPUT_BATCH_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.run_id=p_run AND w.renderer_version='v2.2';
 IF t.id IS NULL OR r.id IS NULL OR project IS NULL OR r.kind<>'v2'
  OR r.status NOT IN ('draft','queued','running','waiting_review') OR r.graph_sealed
  OR r.cancel_requested OR t.cancel_requested OR t.state<>'blocked' OR t.attempt<>0
  OR t.execution_profile ? 'v2_input_manifest' THEN
  RAISE EXCEPTION 'V2_INPUT_TASK_UNAVAILABLE' USING ERRCODE='23514';
 END IF;
 SELECT coalesce(max(i.ordinal)+1,0) INTO next_ordinal FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task;
 IF next_ordinal<>p_first THEN RAISE EXCEPTION 'V2_INPUT_BATCH_GAP' USING ERRCODE='23514'; END IF;
 IF p_first>0 THEN
  SELECT i.artifact_id INTO prior FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.ordinal=p_first-1;
  IF prior IS NULL THEN RAISE EXCEPTION 'V2_INPUT_BATCH_GAP' USING ERRCODE='23514'; END IF;
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_rows) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'ref'-'contract')<>'{}'::jsonb
   OR jsonb_typeof(item->'ref') IS DISTINCT FROM 'object'
   OR ((item->'ref')-'artifact_id'-'sha256')<>'{}'::jsonb
   OR coalesce(item->'ref'->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->'ref'->>'sha256','') !~ '^[0-9a-f]{64}$'
   OR length(coalesce(item->>'contract','')) NOT BETWEEN 1 AND 120
   OR item->>'contract' IN ('InputClosureChunk','InputClosureManifest') THEN
   RAISE EXCEPTION 'V2_INPUT_BATCH_INVALID' USING ERRCODE='22023';
  END IF;
  ref:=item->'ref'; aid:=(ref->>'artifact_id')::uuid;
  IF prior IS NOT NULL AND aid<=prior THEN
   RAISE EXCEPTION 'V2_INPUT_ORDER_INVALID' USING ERRCODE='23514';
  END IF;
  INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
  VALUES(p_tenant,project,p_task,aid,ref->>'sha256',item->>'contract',ordinal);
  prior:=aid; ordinal:=ordinal+1;
 END LOOP;
 RETURN ordinal-p_first;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.seal_v2_task_inputs(
 p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,
 p_entry_count integer,p_entries_sha text,p_direct_roots jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; project uuid; item jsonb;
 n integer; ordinal integer:=0; manifest_id uuid; chunk_id uuid; seen uuid[]:='{}';
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_task IS NULL OR p_entry_count IS NULL
  OR p_entry_count NOT BETWEEN 1 AND 1000000 OR p_entries_sha IS NULL
  OR p_entries_sha !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_manifest) IS DISTINCT FROM 'object'
  OR (p_manifest-'artifact_id'-'sha256')<>'{}'::jsonb
  OR coalesce(p_manifest->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(p_manifest->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_chunks) IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_chunks) NOT BETWEEN 1 AND 1954
  OR jsonb_array_length(p_chunks)<>(p_entry_count+511)/512
  OR jsonb_typeof(p_direct_roots) IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_direct_roots)>127 THEN
  RAISE EXCEPTION 'V2_INPUT_SEAL_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.run_id=p_run AND w.renderer_version='v2.2';
 IF t.id IS NULL OR r.id IS NULL OR project IS NULL OR r.kind<>'v2'
  OR r.status NOT IN ('draft','queued','running','waiting_review') OR r.graph_sealed
  OR r.cancel_requested OR t.cancel_requested OR t.state<>'blocked' OR t.attempt<>0
  OR t.execution_profile ? 'v2_input_manifest' THEN
  RAISE EXCEPTION 'V2_INPUT_TASK_UNAVAILABLE' USING ERRCODE='23514';
 END IF;
 SELECT count(*) INTO n FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task;
 IF n<>p_entry_count OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.ordinal=0)
  OR (SELECT max(i.ordinal) FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task)<>p_entry_count-1
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=t.params_artifact_id) THEN
  RAISE EXCEPTION 'V2_INPUT_ROWS_INCOMPLETE' USING ERRCODE='23514';
 END IF;
 manifest_id:=(p_manifest->>'artifact_id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
  AND d.run_id=p_run AND d.owner_id=p_task AND d.purpose='v2_input_manifest'
  AND d.state='ready' AND d.artifact_id=manifest_id AND d.sha256=p_manifest->>'sha256') THEN
  RAISE EXCEPTION 'V2_INPUT_MANIFEST_UNPUBLISHED' USING ERRCODE='23514';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_chunks) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'artifact_id'-'sha256')<>'{}'::jsonb
   OR coalesce(item->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->>'sha256','') !~ '^[0-9a-f]{64}$' THEN
   RAISE EXCEPTION 'V2_INPUT_SEAL_INVALID' USING ERRCODE='22023';
  END IF;
  chunk_id:=(item->>'artifact_id')::uuid;
  IF chunk_id=manifest_id OR chunk_id=ANY(seen) OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=chunk_id) THEN
   RAISE EXCEPTION 'V2_INPUT_TRANSPORT_COLLISION' USING ERRCODE='23514';
  END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
   AND d.run_id=p_run AND d.owner_id=p_task AND d.purpose='v2_input_chunk_'||ordinal
   AND d.state='ready' AND d.artifact_id=chunk_id AND d.sha256=item->>'sha256') THEN
   RAISE EXCEPTION 'V2_INPUT_CHUNK_UNPUBLISHED' USING ERRCODE='23514';
  END IF;
  INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
  VALUES(p_tenant,project,p_task,chunk_id,item->>'sha256','InputClosureChunk',1000000+ordinal);
  seen:=array_append(seen,chunk_id); ordinal:=ordinal+1;
 END LOOP;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
  AND i.task_id=p_task AND i.artifact_id=manifest_id) THEN
  RAISE EXCEPTION 'V2_INPUT_TRANSPORT_COLLISION' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 VALUES(p_tenant,project,p_task,manifest_id,p_manifest->>'sha256','InputClosureManifest',1000000+ordinal);
 seen:=array_append(seen,manifest_id);
 FOR item IN SELECT value FROM jsonb_array_elements(p_direct_roots) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'artifact_id'-'sha256')<>'{}'::jsonb
   OR coalesce(item->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->>'sha256','') !~ '^[0-9a-f]{64}$'
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
    AND i.task_id=p_task AND i.artifact_id=(item->>'artifact_id')::uuid
    AND i.sha256=item->>'sha256' AND i.contract NOT IN ('InputClosureChunk','InputClosureManifest')) THEN
   RAISE EXCEPTION 'V2_DIRECT_ROOT_INVALID' USING ERRCODE='23514';
  END IF;
 END LOOP;
 IF (SELECT count(*) FROM jsonb_array_elements(p_direct_roots) x) <>
  (SELECT count(DISTINCT x.value->>'artifact_id') FROM jsonb_array_elements(p_direct_roots) x) THEN
  RAISE EXCEPTION 'V2_DIRECT_ROOT_DUPLICATE' USING ERRCODE='23514';
 END IF;
 UPDATE photo.tasks SET inputs=jsonb_build_array(p_manifest)||p_direct_roots,
  execution_profile=execution_profile || jsonb_build_object(
   'renderer_version','v2.2',
   'contracts_sha','4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4',
   'v2_input_manifest',p_manifest,'v2_entry_count',p_entry_count,'v2_entries_sha',p_entries_sha)
 WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT photo.v2_input_closure_ready(p_task) THEN
  RAISE EXCEPTION 'V2_INPUT_SEAL_INCOMPLETE' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.guard_sealed_v2_task_inputs() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.execution_profile ? 'v2_input_manifest' AND (
  NEW.inputs IS DISTINCT FROM OLD.inputs OR NEW.params_artifact_id IS DISTINCT FROM OLD.params_artifact_id
  OR NEW.input_digest IS DISTINCT FROM OLD.input_digest
  OR NEW.execution_profile->'v2_input_manifest' IS DISTINCT FROM OLD.execution_profile->'v2_input_manifest'
  OR NEW.execution_profile->'contracts_sha' IS DISTINCT FROM OLD.execution_profile->'contracts_sha'
  OR NEW.execution_profile->'renderer_version' IS DISTINCT FROM OLD.execution_profile->'renderer_version'
  OR NEW.execution_profile->'v2_entry_count' IS DISTINCT FROM OLD.execution_profile->'v2_entry_count'
  OR NEW.execution_profile->'v2_entries_sha' IS DISTINCT FROM OLD.execution_profile->'v2_entries_sha') THEN
  RAISE EXCEPTION 'V2_TASK_INPUTS_IMMUTABLE' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_sealed_v2_task_inputs() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_sealed_v2_task_inputs() FROM PUBLIC;
CREATE TRIGGER v2_task_input_seal_guard BEFORE UPDATE ON photo.tasks
 FOR EACH ROW EXECUTE FUNCTION photo.guard_sealed_v2_task_inputs();
