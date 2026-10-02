-- +goose Up
-- Pure plan validation, without lineage authority, persistence or dispatch.
-- Exact params text is retained inside JSON so PostgreSQL never reserializes
-- jsonb to derive the SHA of a published canonical document.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_plan_shape(p_workflow uuid,p_config jsonb,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb; params jsonb; wire text; n integer:=0; total numeric:=0;
 h bytea; expected uuid; width bigint; height bigint; identifier text;
BEGIN
 IF p_workflow IS NULL OR p_workflow='00000000-0000-0000-0000-000000000000'::uuid
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 1 AND 9007199254740991
  OR jsonb_typeof(p_config) IS DISTINCT FROM 'object'
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_config) k)
   IS DISTINCT FROM ARRAY['input_snapshot_id','project_id','v25_composition']::text[]
  OR octet_length(p_config::text)>2097152 THEN RETURN false;END IF;
 FOR identifier IN SELECT p_config->>'project_id' UNION ALL SELECT p_config->>'input_snapshot_id'
  UNION ALL SELECT p_config->'v25_composition'->>'auth_session_id' LOOP
  IF coalesce(identifier,'') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR identifier='00000000-0000-0000-0000-000000000000' THEN RETURN false;END IF;
 END LOOP;
 IF jsonb_typeof(p_config->'v25_composition') IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_config->'v25_composition') k)
  IS DISTINCT FROM ARRAY['auth_session_id','nodes']::text[]
  OR jsonb_typeof(p_config->'v25_composition'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_config->'v25_composition'->'nodes') NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p_config->'v25_composition'->'nodes') LOOP
  IF jsonb_typeof(node) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
  IF (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(node) k)
   IS DISTINCT FROM ARRAY['height','input_digest','ordinal','params','params_jcs','profile','task_id','width']::text[]
   OR jsonb_typeof(node->'ordinal') IS DISTINCT FROM 'number'
   OR node->>'ordinal' IS DISTINCT FROM n::text
   OR jsonb_typeof(node->'task_id') IS DISTINCT FROM 'string'
   OR coalesce(node->>'input_digest','') !~ '^[0-9a-f]{64}$'
   OR jsonb_typeof(node->'input_digest') IS DISTINCT FROM 'string'
   OR NOT photo.v25_layer_ref_shape(node->'params')
   OR jsonb_typeof(node->'params_jcs') IS DISTINCT FROM 'string'
   OR jsonb_typeof(node->'width') IS DISTINCT FROM 'number'
   OR jsonb_typeof(node->'height') IS DISTINCT FROM 'number'
   OR coalesce(node->>'width','') !~ '^[1-9][0-9]{0,8}$'
   OR coalesce(node->>'height','') !~ '^[1-9][0-9]{0,8}$' THEN RETURN false;END IF;
  h:=public.digest(uuid_send(p_workflow)||convert_to('layer.compose:'||n::text,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF node->>'task_id' IS DISTINCT FROM expected::text THEN RETURN false;END IF;
  wire:=node->>'params_jcs';
  IF octet_length(wire) NOT BETWEEN 2 AND 1048576
   OR encode(sha256(convert_to(wire,'UTF8')),'hex') IS DISTINCT FROM node->'params'->>'sha256' THEN RETURN false;END IF;
  params:=wire::jsonb;width:=(node->>'width')::bigint;height:=(node->>'height')::bigint;
  IF NOT photo.v25_layer_profile_shape(params,width,height,node->'profile') THEN RETURN false;END IF;
  total:=total+(node->'profile'->'resource_estimate'->>'output_bytes')::numeric;
  IF total>9007199254740991 THEN RETURN false;END IF;
  n:=n+1;
 END LOOP;
 RETURN total=p_bytes;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_plan_shape(uuid,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_plan_shape(uuid,jsonb,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_plan_shape(uuid,jsonb,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
