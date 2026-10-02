-- +goose Up
-- Repeat structural render root/source proof on every closure readiness check.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_input_closure_ready(p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; manifest jsonb; ref jsonb; direct_count integer; entry_count integer; node jsonb; binding jsonb; config jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND OR jsonb_typeof(t.inputs) IS DISTINCT FROM 'array'
  OR (jsonb_typeof(t.inputs)='array' AND jsonb_array_length(t.inputs) NOT BETWEEN 1 AND 128)
  OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR coalesce(t.execution_profile->>'contracts_sha','') NOT IN (
   '6c52d2a8828f7370520064925c29bb4a043be05d34934db612e7b1fb9016fad2',
   '341984c53d787175fb5a9eec4e5d55ab74e9cd293075add8965d6064aba2e815',
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
 IF t.operation='render.compose' THEN
  IF t.operation_version<>'1' OR entry_count NOT BETWEEN 2 AND 4096
   OR jsonb_array_length(t.inputs)<>2
   OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '6c52d2a8828f7370520064925c29bb4a043be05d34934db612e7b1fb9016fad2' THEN RETURN false;END IF;
  SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
  IF jsonb_typeof(config->'v26_render'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT value INTO node FROM jsonb_array_elements(config->'v26_render'->'nodes') WHERE value->>'task_id'=t.id::text;
  IF node IS NULL OR t.params_artifact_id::text IS DISTINCT FROM node->'params'->>'artifact_id'
   OR t.inputs->1 IS DISTINCT FROM node->'params' THEN RETURN false;END IF;
  binding:=(node->>'params_jcs')::jsonb->'scalars'->'source';
  IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id AND contract='FrozenRenderRecipe')<>1
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.contract='FrozenRenderRecipe' AND i.artifact_id=t.params_artifact_id AND i.sha256=node->'params'->>'sha256')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
    AND i.contract='WorkingImage' AND i.artifact_id::text=binding->'source'->>'artifact_id'
    AND i.sha256=binding->'source'->>'sha256') THEN RETURN false;END IF;
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
