-- +goose Up
-- Private node predicate for future atomic approved-child admission. The caller
-- must separately validate semantic_approved_child_params_ready and exact source
-- documents/closure, reserve quota and require operation activation. No run or
-- task is created or dispatched by this predicate.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_approved_exposure_nodes_ready(p_tenant uuid,p_project uuid,p_child uuid,p_params jsonb,p_nodes jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb; param jsonb; profile jsonb; estimate jsonb; field text; pixels bigint; n integer;
BEGIN
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_params) IS DISTINCT FROM 'array' OR jsonb_typeof(p_nodes) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_params);
 IF n NOT BETWEEN 1 AND 100 OR jsonb_array_length(p_nodes)<>n
 OR (SELECT count(DISTINCT p->>'task_id') FROM jsonb_array_elements(p_params) p)<>n
 OR (SELECT count(DISTINCT p->>'target_id') FROM jsonb_array_elements(p_params) p)<>n
 OR (SELECT count(DISTINCT p->>'id') FROM jsonb_array_elements(p_nodes) p)<>n THEN RETURN false;END IF;
 -- Stable task UUID order also keeps qualification lock acquisition repeatable.
 FOR node IN SELECT value FROM jsonb_array_elements(p_nodes) ORDER BY value->>'id' LOOP
  IF jsonb_typeof(node) IS DISTINCT FROM 'object'
  OR NOT(node ?& ARRAY['id','node_key','operation','operation_version','resource_class','params_artifact_id','params_sha','input_digest','execution_profile'])
  OR (node-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile')<>'{}'::jsonb THEN RETURN false;END IF;
  FOREACH field IN ARRAY ARRAY['id','node_key','operation','operation_version','resource_class','params_artifact_id','params_sha','input_digest'] LOOP
   IF jsonb_typeof(node->field) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
  END LOOP;
  SELECT value INTO param FROM jsonb_array_elements(p_params) WHERE value->>'task_id'=node->>'id';
  IF param IS NULL OR (node->>'id')::uuid IS DISTINCT FROM photo.semantic_uuid_v5(p_child,'semantic.edit:'||(param->>'target_id')::uuid::text)
  OR node->>'node_key' IS DISTINCT FROM 'semantic.edit:'||(param->>'target_id')::uuid::text
  OR node->>'params_artifact_id' IS DISTINCT FROM param->>'artifact_id'
  OR (node->>'params_artifact_id')::uuid IS DISTINCT FROM photo.semantic_uuid_v5((node->>'id')::uuid,'Params')
  OR node->>'params_sha' IS DISTINCT FROM param->>'sha256'
  OR coalesce(node->>'params_sha','') !~ '^[0-9a-f]{64}$' OR coalesce(node->>'input_digest','') !~ '^[0-9a-f]{64}$'
  OR node->>'operation' IS DISTINCT FROM 'semantic.local_exposure' OR node->>'operation_version' IS DISTINCT FROM '1'
  OR node->>'resource_class' IS DISTINCT FROM 'cpu' OR (param->>'body')::jsonb->>'kind' IS DISTINCT FROM 'local_exposure' THEN RETURN false;END IF;
  profile:=node->'execution_profile';estimate:=profile->'resource_estimate';
  IF jsonb_typeof(profile) IS DISTINCT FROM 'object' OR profile->>'backend' IS DISTINCT FROM 'cpu'
  OR NOT(profile ?& ARRAY['backend','renderer_version','resource_estimate','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','qualification'])
  OR (profile-'backend'-'renderer_version'-'resource_estimate'-'model_lock_sha'-'runtime_lock_sha'-'device_profile_sha'-'dataset_sha'-'qualification')<>'{}'::jsonb
  OR jsonb_typeof(estimate) IS DISTINCT FROM 'object'
  OR NOT(estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
  OR (estimate-'ram_bytes'-'vram_bytes'-'scratch_bytes'-'output_bytes'-'pixel_count')<>'{}'::jsonb THEN RETURN false;END IF;
  FOREACH field IN ARRAY ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'] LOOP
   IF jsonb_typeof(estimate->field) IS DISTINCT FROM 'number' OR (estimate->>field) !~ '^(0|[1-9][0-9]*)$'
   OR (estimate->>field)::numeric>9007199254740991 THEN RETURN false;END IF;
  END LOOP;
  pixels:=(estimate->>'pixel_count')::bigint;
  IF pixels NOT BETWEEN 1 AND 200000000 OR (estimate->>'vram_bytes')::bigint<>0
  OR (estimate->>'ram_bytes')::bigint < 204*pixels+67108864+1
  OR (estimate->>'scratch_bytes')::bigint < 20*pixels+3*16384+16777216
  OR (estimate->>'output_bytes')::bigint < 12*pixels+16384+3*1048576 THEN RETURN false;END IF;
  IF NOT photo.v27_downstream_profile_ready(p_tenant,p_project,'semantic.local_exposure','1','cpu',profile) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_approved_exposure_nodes_ready(uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_approved_exposure_nodes_ready(uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
