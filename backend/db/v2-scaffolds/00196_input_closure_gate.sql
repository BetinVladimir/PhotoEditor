-- A V2 task is dispatchable only after the trusted compiler has persisted
-- its exact input inventory and transport metadata. The worker still verifies
-- manifest/chunk content and the gateway checks every requested artifact.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_input_closure_ready(p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; manifest jsonb; ref jsonb; direct_count integer; entry_count integer;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND OR jsonb_typeof(t.inputs) IS DISTINCT FROM 'array'
  OR (jsonb_typeof(t.inputs)='array' AND jsonb_array_length(t.inputs) NOT BETWEEN 1 AND 128)
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM
   '4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4' THEN
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
ALTER FUNCTION photo.v2_input_closure_ready(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_closure_ready(uuid) FROM PUBLIC;
