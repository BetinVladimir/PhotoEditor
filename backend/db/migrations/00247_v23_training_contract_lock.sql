-- +goose Up
-- Forward contract lock update after RenderCase pins its source documents.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_input_closure_ready(p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; manifest jsonb; ref jsonb; direct_count integer; entry_count integer;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND OR jsonb_typeof(t.inputs) IS DISTINCT FROM 'array'
  OR (jsonb_typeof(t.inputs)='array' AND jsonb_array_length(t.inputs) NOT BETWEEN 1 AND 128)
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR coalesce(t.execution_profile->>'contracts_sha','') NOT IN (
   '266da073ca9bf6863ac9f9a5f54b00f1ee702a415fd91fa735afea648428e685',
   '4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4') THEN
  RETURN false;
 END IF;
 IF coalesce(t.execution_profile->>'v2_entry_count','') !~ '^[1-9][0-9]{0,6}$'
  OR coalesce(t.execution_profile->>'v2_entries_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 entry_count:=(t.execution_profile->>'v2_entry_count')::integer;
 IF entry_count>1000000 THEN RETURN false; END IF;
 manifest:=t.execution_profile->'v2_input_manifest';
 IF jsonb_typeof(manifest) IS DISTINCT FROM 'object' OR (manifest-'artifact_id'-'sha256')<>'{}'::jsonb
  OR manifest->>'artifact_id' IS NULL OR manifest->>'sha256' IS NULL
  OR manifest->>'sha256' !~ '^[0-9a-f]{64}$' THEN
  RETURN false;
 END IF;
 SELECT count(*) INTO direct_count FROM jsonb_array_elements(t.inputs) AS x(ref)
  WHERE x.ref=manifest;
 IF direct_count<>1 THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='v2'
   AND w.renderer_version='v2.2') THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract='InputClosureManifest' AND i.artifact_id::text=manifest->>'artifact_id'
     AND i.sha256=manifest->>'sha256')<>1 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract='InputClosureChunk')<>(entry_count+511)/512 THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
     AND i.contract NOT IN ('InputClosureManifest','InputClosureChunk'))<>entry_count THEN
  RETURN false;
 END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(ref)<>'object' OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb
   OR ref->>'artifact_id' IS NULL OR ref->>'sha256' IS NULL
   OR ref->>'sha256' !~ '^[0-9a-f]{64}$' THEN
   RETURN false;
  END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.artifact_id::text=ref->>'artifact_id' AND i.sha256=ref->>'sha256') THEN
   RETURN false;
  END IF;
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_v2_task_inputs(
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
   'contracts_sha','266da073ca9bf6863ac9f9a5f54b00f1ee702a415fd91fa735afea648428e685',
   'v2_input_manifest',p_manifest,'v2_entry_count',p_entry_count,'v2_entries_sha',p_entries_sha)
 WHERE tenant_id=p_tenant AND id=p_task;
 IF NOT photo.v2_input_closure_ready(p_task) THEN
  RAISE EXCEPTION 'V2_INPUT_SEAL_INCOMPLETE' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
