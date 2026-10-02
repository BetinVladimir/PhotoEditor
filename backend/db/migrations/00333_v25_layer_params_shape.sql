-- +goose Up
-- Pure frozen-wire validation. This does not authorize any artifact or task.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_ref_shape(p_ref jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_ref)='object'
  AND ARRAY(SELECT jsonb_object_keys(CASE WHEN jsonb_typeof(p_ref)='object' THEN p_ref ELSE '{}'::jsonb END) ORDER BY 1)=ARRAY['artifact_id','sha256']
  AND jsonb_typeof(p_ref->'artifact_id')='string'
  AND p_ref->>'artifact_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  AND p_ref->>'artifact_id'<>'00000000-0000-0000-0000-000000000000'
  AND jsonb_typeof(p_ref->'sha256')='string' AND p_ref->>'sha256' ~ '^[0-9a-f]{64}$',false)
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_params_shape(p_params jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE base jsonb; node jsonb; operation text; stack jsonb[]; depths integer[];
 depth integer; count integer:=0; position integer; canvas text;
BEGIN
 IF p_params IS NULL OR jsonb_typeof(p_params)<>'object'
  OR octet_length(p_params::text)>1048576 THEN RETURN false; END IF;
 IF ARRAY(SELECT jsonb_object_keys(p_params) ORDER BY 1)<>ARRAY['base','blend','mask','opacity','proposal_result','protected_mask','schema_version','source_result']
  OR p_params->'schema_version' IS DISTINCT FROM '1'::jsonb
  OR jsonb_typeof(p_params->'blend') IS DISTINCT FROM 'string'
  OR p_params->>'blend' NOT IN('normal','multiply','screen')
  OR jsonb_typeof(p_params->'opacity') IS DISTINCT FROM 'number' THEN RETURN false; END IF;
 IF (p_params->>'opacity')::numeric NOT BETWEEN 0 AND 1 THEN RETURN false; END IF;
 IF NOT photo.v25_layer_ref_shape(p_params->'proposal_result')
  OR (p_params->'source_result'<>'null'::jsonb AND NOT photo.v25_layer_ref_shape(p_params->'source_result'))
  OR (p_params->'protected_mask'<>'null'::jsonb AND NOT photo.v25_layer_ref_shape(p_params->'protected_mask')) THEN RETURN false; END IF;
 base:=p_params->'base';
 IF jsonb_typeof(base) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
 IF ARRAY(SELECT jsonb_object_keys(base) ORDER BY 1)<>ARRAY['asset_version_id','canvas_hash','source']
  OR NOT photo.v25_layer_ref_shape(base->'source')
  OR jsonb_typeof(base->'asset_version_id') IS DISTINCT FROM 'string'
  OR coalesce(base->>'asset_version_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR base->>'asset_version_id'='00000000-0000-0000-0000-000000000000'
  OR jsonb_typeof(base->'canvas_hash') IS DISTINCT FROM 'string'
  OR coalesce(base->>'canvas_hash','') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 canvas:=base->>'canvas_hash';stack:=ARRAY[p_params->'mask'];depths:=ARRAY[0];
 WHILE coalesce(array_length(stack,1),0)>0 LOOP
  position:=array_length(stack,1);node:=stack[position];depth:=depths[position];
  stack:=stack[1:position-1];depths:=depths[1:position-1];count:=count+1;
  IF count>256 OR depth>16 OR jsonb_typeof(node) IS DISTINCT FROM 'object'
   OR jsonb_typeof(node->'op') IS DISTINCT FROM 'string' THEN RETURN false; END IF;
  operation:=node->>'op';
  IF operation='ref' THEN
   IF ARRAY(SELECT jsonb_object_keys(node) ORDER BY 1)<>ARRAY['canvas_hash','mask','op']
    OR NOT photo.v25_layer_ref_shape(node->'mask')
    OR jsonb_typeof(node->'canvas_hash') IS DISTINCT FROM 'string'
    OR node->>'canvas_hash' IS DISTINCT FROM canvas THEN RETURN false; END IF;
  ELSIF operation='invert' THEN
   IF ARRAY(SELECT jsonb_object_keys(node) ORDER BY 1)<>ARRAY['op','operand'] THEN RETURN false; END IF;
   stack:=array_append(stack,node->'operand');depths:=array_append(depths,depth+1);
  ELSIF operation IN('union','intersect','subtract') THEN
   IF ARRAY(SELECT jsonb_object_keys(node) ORDER BY 1)<>ARRAY['left','op','right'] THEN RETURN false; END IF;
   stack:=array_append(array_append(stack,node->'right'),node->'left');depths:=array_append(array_append(depths,depth+1),depth+1);
  ELSE RETURN false;
  END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_ref_shape(jsonb) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v25_layer_params_shape(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_ref_shape(jsonb),photo.v25_layer_params_shape(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_ref_shape(jsonb),photo.v25_layer_params_shape(jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
