-- +goose Up
-- Pure resource calculation for frozen 512px CPU composition tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_resources(p_params jsonb,p_width bigint,p_height bigint)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE stack jsonb[]; nodes jsonb[]:=ARRAY[]::jsonb[]; refs jsonb[]:=ARRAY[]::jsonb[];
 node jsonb; item jsonb; operation text; position integer; pixels bigint; tile bigint;
 mapped bigint; temporary bigint;
BEGIN
 IF p_width IS NULL OR p_height IS NULL OR p_width<1 OR p_height<1
  OR p_width>200000000 THEN RETURN NULL; END IF;
 IF p_height>200000000/p_width OR NOT photo.v25_layer_params_shape(p_params) THEN RETURN NULL; END IF;
 stack:=ARRAY[p_params->'mask'];
 WHILE coalesce(array_length(stack,1),0)>0 LOOP
  position:=array_length(stack,1);node:=stack[position];stack:=stack[1:position-1];
  IF node=ANY(nodes) THEN CONTINUE; END IF;
  nodes:=array_append(nodes,node);operation:=node->>'op';
  IF operation='ref' THEN
   item:=node->'mask';IF NOT item=ANY(refs) THEN refs:=array_append(refs,item);END IF;
  ELSIF operation='invert' THEN stack:=array_append(stack,node->'operand');
  ELSE stack:=array_append(array_append(stack,node->'right'),node->'left');
  END IF;
 END LOOP;
 item:=p_params->'protected_mask';
 IF item<>'null'::jsonb AND NOT item=ANY(refs) THEN refs:=array_append(refs,item);END IF;
 pixels:=p_width*p_height;tile:=least(p_width,512::bigint)*least(p_height,512::bigint);
 mapped:=(68+4*array_length(refs,1)::bigint)*pixels;
 temporary:=greatest(256::bigint,4*(array_length(nodes,1)::bigint+3))*tile;
 RETURN jsonb_build_object('pixel_count',pixels,'ram_bytes',mapped+temporary+268435456,
  'scratch_bytes',mapped+67108864,'output_bytes',24*pixels+33554432,'vram_bytes',0);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_profile_shape(p_params jsonb,p_width bigint,p_height bigint,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE resources jsonb;
BEGIN
 resources:=photo.v25_layer_resources(p_params,p_width,p_height);
 IF resources IS NULL OR p_profile IS NULL OR jsonb_typeof(p_profile)<>'object' THEN RETURN false;END IF;
 RETURN p_profile=jsonb_build_object('backend','cpu','renderer_version','v2.2','resource_estimate',resources);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_resources(jsonb,bigint,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_resources(jsonb,bigint,bigint),photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_resources(jsonb,bigint,bigint),photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
