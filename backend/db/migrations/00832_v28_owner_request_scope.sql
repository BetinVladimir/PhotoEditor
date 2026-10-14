-- +goose Up
-- Scheduler-only prepublication scope. Does not create an owner or grant IO.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_owner_request_scope(p_request jsonb)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE intent jsonb;params jsonb;wire text;operation text;entry jsonb;prior text;identifier text;
 tenant uuid;project uuid;snapshot uuid;actor uuid;session_id uuid;workflow uuid;ordinal integer;
 width bigint;height bigint;resources jsonb;keys text[];
BEGIN
 IF jsonb_typeof(p_request) IS DISTINCT FROM 'object' OR octet_length(p_request::text)>2097152
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_request) k) IS DISTINCT FROM
   ARRAY['actor_id','auth_session_id','guard_pixels','height','intent','manual_root','ordinal','original_params','project_id','run_id','schema_version','snapshot_id','target_pixels','tenant_id','vectors','width','workflow_id']::text[]
  OR p_request->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 FOREACH identifier IN ARRAY ARRAY[p_request->>'tenant_id',p_request->>'project_id',p_request->>'snapshot_id',p_request->>'actor_id',
  p_request->>'auth_session_id',p_request->>'run_id',p_request->>'workflow_id'] LOOP
  IF coalesce(identifier,'') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR identifier='00000000-0000-0000-0000-000000000000' THEN RETURN false;END IF;
 END LOOP;
 tenant:=(p_request->>'tenant_id')::uuid;project:=(p_request->>'project_id')::uuid;snapshot:=(p_request->>'snapshot_id')::uuid;
 actor:=(p_request->>'actor_id')::uuid;session_id:=(p_request->>'auth_session_id')::uuid;workflow:=(p_request->>'workflow_id')::uuid;
 IF jsonb_typeof(p_request->'ordinal') IS DISTINCT FROM 'number' OR (p_request->>'ordinal') !~ '^[0-9]+$'
  OR jsonb_typeof(p_request->'width') IS DISTINCT FROM 'number' OR (p_request->>'width') !~ '^[0-9]+$'
  OR jsonb_typeof(p_request->'height') IS DISTINCT FROM 'number' OR (p_request->>'height') !~ '^[0-9]+$' THEN RETURN false;END IF;
 ordinal:=(p_request->>'ordinal')::integer;width:=(p_request->>'width')::bigint;height:=(p_request->>'height')::bigint;
 IF ordinal NOT BETWEEN 0 AND 4999 OR workflow IS DISTINCT FROM photo.semantic_uuid_v5((p_request->>'run_id')::uuid,'workflow') THEN RETURN false;END IF;
 intent:=p_request->'intent';
 IF jsonb_typeof(intent) IS DISTINCT FROM 'object' OR intent->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(intent) k) IS DISTINCT FROM
   ARRAY['binding','manual_entries','node_key','operation','params_jcs','params_sha','profile','reserved_bytes','schema_version','task_id']::text[] THEN RETURN false;END IF;
 operation:=intent->>'operation';wire:=intent->>'params_jcs';
 IF operation IS NULL OR operation NOT IN('portrait.hair','portrait.clothing')
  OR intent->>'node_key' IS DISTINCT FROM operation||':'||ordinal::text
  OR intent->>'task_id' IS DISTINCT FROM photo.semantic_uuid_v5(workflow,operation||':'||ordinal::text)::text
  OR jsonb_typeof(intent->'params_jcs') IS DISTINCT FROM 'string' OR octet_length(wire) NOT BETWEEN 2 AND 65536
  OR intent->>'params_sha' IS DISTINCT FROM encode(sha256(convert_to(wire,'UTF8')),'hex') THEN RETURN false;END IF;
 params:=wire::jsonb;
 IF jsonb_typeof(params) IS DISTINCT FROM 'object' OR params->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR params->'base' IS DISTINCT FROM intent->'binding' THEN RETURN false;END IF;
 keys:=CASE WHEN operation='portrait.hair' THEN ARRAY['base','detail','hue_delta_degrees','mask','protected','saturation_factor','schema_version','strength']
  ELSE ARRAY['base','hue_delta_degrees','mask','protected','saturation_factor','schema_version','strength','wrinkle_strength'] END;
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(params) k) IS DISTINCT FROM keys THEN RETURN false;END IF;
 FOREACH identifier IN ARRAY ARRAY['strength','hue_delta_degrees','saturation_factor',CASE WHEN operation='portrait.hair' THEN 'detail' ELSE 'wrinkle_strength' END] LOOP
  IF jsonb_typeof(params->identifier) IS DISTINCT FROM 'number' THEN RETURN false;END IF;
 END LOOP;
 IF (params->>'strength')::numeric NOT BETWEEN 0 AND 1
  OR (params->>CASE WHEN operation='portrait.hair' THEN 'detail' ELSE 'wrinkle_strength' END)::numeric NOT BETWEEN 0 AND 1
  OR (params->>'hue_delta_degrees')::numeric NOT BETWEEN (CASE WHEN operation='portrait.hair' THEN -30 ELSE -180 END) AND (CASE WHEN operation='portrait.hair' THEN 30 ELSE 180 END)
  OR (params->>'saturation_factor')::numeric NOT BETWEEN (CASE WHEN operation='portrait.hair' THEN 0.7 ELSE 0 END) AND (CASE WHEN operation='portrait.hair' THEN 1.3 ELSE 2 END) THEN RETURN false;END IF;
 FOREACH entry IN ARRAY ARRAY[params->'mask',params->'protected',p_request->'manual_root',p_request->'original_params',p_request->'target_pixels',p_request->'guard_pixels'] LOOP
  IF photo.v25_layer_ref_shape(entry) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 IF params->'mask'->>'artifact_id'=params->'protected'->>'artifact_id' THEN RETURN false;END IF;
 resources:=photo.v28_local_portrait_resources(width,height);
 IF resources IS NULL OR intent->'reserved_bytes' IS DISTINCT FROM resources->'output_bytes'
  OR photo.v28_local_portrait_profile_shape(width,height,intent->'profile'->>'portrait_runtime_sha',intent->'profile') IS DISTINCT FROM true
  OR photo.v28_local_portrait_source_scope(tenant,project,snapshot,actor,session_id,intent->'binding') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v28_manual_pair_origin(tenant,project,snapshot,actor,session_id,intent->'binding',
  (params->'mask'->>'artifact_id')::uuid,params->'mask'->>'sha256',(params->'protected'->>'artifact_id')::uuid,params->'protected'->>'sha256',
  (p_request->'target_pixels'->>'artifact_id')::uuid,p_request->'target_pixels'->>'sha256',(p_request->'guard_pixels'->>'artifact_id')::uuid,p_request->'guard_pixels'->>'sha256') origin
  WHERE origin.root_id::text=p_request->'manual_root'->>'artifact_id' AND origin.root_sha=p_request->'manual_root'->>'sha256'
   AND origin.params_id::text=p_request->'original_params'->>'artifact_id' AND origin.params_sha=p_request->'original_params'->>'sha256') THEN RETURN false;END IF;
 IF p_request->'vectors'<>'null'::jsonb THEN
  IF photo.v25_layer_ref_shape(p_request->'vectors') IS DISTINCT FROM true OR photo.v28_manual_vectors_scope(tenant,project,snapshot,actor,session_id,intent->'binding',
   (params->'mask'->>'artifact_id')::uuid,params->'mask'->>'sha256',(params->'protected'->>'artifact_id')::uuid,params->'protected'->>'sha256',
   (p_request->'target_pixels'->>'artifact_id')::uuid,p_request->'target_pixels'->>'sha256',(p_request->'guard_pixels'->>'artifact_id')::uuid,p_request->'guard_pixels'->>'sha256',
   (p_request->'manual_root'->>'artifact_id')::uuid,p_request->'manual_root'->>'sha256',(p_request->'original_params'->>'artifact_id')::uuid,p_request->'original_params'->>'sha256',
   (p_request->'vectors'->>'artifact_id')::uuid,p_request->'vectors'->>'sha256') IS DISTINCT FROM true THEN RETURN false;END IF;
 END IF;
 IF jsonb_typeof(intent->'manual_entries') IS DISTINCT FROM 'array' OR jsonb_array_length(intent->'manual_entries') NOT BETWEEN 1 AND 126 THEN RETURN false;END IF;
 IF NOT (intent->'manual_entries' @> jsonb_build_array(
  jsonb_build_object('ref',intent->'binding'->'source','contract','WorkingImage'),
  jsonb_build_object('ref',params->'mask','contract','MaskRef'),jsonb_build_object('ref',params->'protected','contract','MaskRef'),
  jsonb_build_object('ref',p_request->'manual_root','contract','RasterizedMask'),jsonb_build_object('ref',p_request->'original_params','contract','Params'),
  jsonb_build_object('ref',p_request->'target_pixels','contract','MaskPixels'),jsonb_build_object('ref',p_request->'guard_pixels','contract','MaskPixels')))
 THEN RETURN false;END IF;
 IF p_request->'vectors'<>'null'::jsonb AND NOT (intent->'manual_entries' @> jsonb_build_array(jsonb_build_object('ref',p_request->'vectors','contract','ManualMask')))
 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(intent->'manual_entries') LOOP
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(entry) k) IS DISTINCT FROM ARRAY['contract','ref']::text[]
   OR photo.v25_layer_ref_shape(entry->'ref') IS DISTINCT FROM true OR jsonb_typeof(entry->'contract') IS DISTINCT FROM 'string'
   OR length(entry->>'contract') NOT BETWEEN 1 AND 120 OR (prior IS NOT NULL AND prior>=entry->'ref'->>'artifact_id') THEN RETURN false;END IF;
  prior:=entry->'ref'->>'artifact_id';
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_local_owner_request_scope(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_owner_request_scope(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_local_owner_request_scope(jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
