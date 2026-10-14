-- +goose Up
-- Pure conservative CPU budget mirror of MultiResources; grants no source authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_resource_estimate(p_operation text,p_params jsonb,p_sources jsonb)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
DECLARE n bigint;reference integer;maximum bigint;image jsonb;step jsonb;raw text;i integer;
 h bigint;w bigint;pixels bigint;source_pixels bigint:=0;downloads bigint:=0;max_source bigint:=0;max_width bigint:=0;
 ref_h bigint;ref_w bigint;output bigint;width bigint;source_disk bigint;source_ram bigint;proxy_ram bigint;
 registration_disk bigint;stage_disk bigint;stage_ram bigint;planar_ram bigint;projection_disk bigint:=0;projection_ram bigint:=0;
 encoded_disk bigint;encoding_ram bigint;ram bigint;scratch bigint;out_bytes bigint;
 levels integer;lg integer:=0;small bigint;total bigint;coarse bigint:=0;fine bigint:=0;length bigint;arrays bigint;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN ('optical.panorama','optical.focus_stack')
 OR jsonb_typeof(p_params->'frames') IS DISTINCT FROM 'array' OR jsonb_typeof(p_sources) IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 n:=jsonb_array_length(p_sources);
 IF n NOT BETWEEN 2 AND CASE p_operation WHEN 'optical.panorama' THEN 20 ELSE 30 END
 OR jsonb_array_length(p_params->'frames')<>n THEN RETURN NULL;END IF;
 reference:=(p_params->>'reference_ordinal')::integer;maximum:=(p_params->>'max_pixels')::bigint;
 IF reference IS NULL OR reference NOT BETWEEN 0 AND n-1 OR maximum IS NULL OR maximum NOT BETWEEN 1 AND 200000000 THEN RETURN NULL;END IF;
 FOR i IN 0..n::integer-1 LOOP
  IF jsonb_typeof(p_sources->i) IS DISTINCT FROM 'string' THEN RETURN NULL;END IF;
  raw:=p_sources->>i;
  IF octet_length(raw) NOT BETWEEN 2 AND 1048576 OR encode(sha256(convert_to(raw,'UTF8')),'hex') IS DISTINCT FROM p_params->'frames'->i->'source'->>'sha256' THEN RETURN NULL;END IF;
  image:=raw::jsonb;h:=(image->'canvas'->>'height')::bigint;w:=(image->'canvas'->>'width')::bigint;
  IF h IS NULL OR w IS NULL OR h<8 OR w<8 OR h>200000000/w OR image->>'dtype' IS DISTINCT FROM 'float32' THEN RETURN NULL;END IF;
  pixels:=h*w;source_pixels:=source_pixels+pixels;max_source:=greatest(max_source,pixels);max_width:=greatest(max_width,w);
  IF i=reference THEN ref_h:=h;ref_w:=w;END IF;
  downloads:=downloads+12*pixels+65536;
  IF image->'alpha' IS NOT NULL AND image->'alpha'<>'null'::jsonb THEN downloads:=downloads+4*pixels+65536;END IF;
  IF jsonb_typeof(image->'transform_chain'->'steps') IS DISTINCT FROM 'array' OR jsonb_array_length(image->'transform_chain'->'steps')>128 THEN RETURN NULL;END IF;
  FOR step IN SELECT value FROM jsonb_array_elements(image->'transform_chain'->'steps') LOOP
   IF step->>'kind'='dense_warp' THEN
    h:=(step->'to_canvas'->>'height')::bigint;w:=(step->'to_canvas'->>'width')::bigint;
    IF h IS NULL OR w IS NULL OR h<1 OR w<1 OR h>200000000/w THEN RETURN NULL;END IF;
    downloads:=downloads+8*h*w+65536;
   END IF;
  END LOOP;
 END LOOP;
 IF p_operation='optical.panorama' THEN output:=maximum;width:=output;
 ELSE output:=ref_h*ref_w;width:=ref_w;IF output>maximum THEN RETURN NULL;END IF;END IF;
 source_disk:=16*source_pixels+2*n*4096;
 source_ram:=16*source_pixels+source_disk+128*256*256+268435456;
 proxy_ram:=16*source_pixels+(4*n+16)*2048*2048+268435456;
 registration_disk:=n*(25*output+4*4096);
 IF p_operation='optical.panorama' THEN
  stage_disk:=17*output+3*4096;
  stage_ram:=17*n*output+stage_disk+256::bigint*384*384+16*width+268435456;
 ELSE
  h:=ref_h;w:=ref_w;small:=least(h,w);
  WHILE small>1 LOOP lg:=lg+1;small:=small/2;END LOOP;
  levels:=greatest(1,least(6,lg-4));total:=h*w;
  FOR i IN 1..levels LOOP
   fine:=fine+h*w;h:=(h+1)/2;w:=(w+1)/2;coarse:=coarse+h*w;total:=total+h*w;
  END LOOP;
  length:=levels+1;arrays:=n*(3*length-2)+3*length+2;
  stage_disk:=n*(24*coarse+8*total+24*fine)+24*fine+48*total+17*output+arrays*4096;
  stage_ram:=17*n*output+stage_disk+256::bigint*522*522+16*width+268435456;
 END IF;
 planar_ram:=16*source_pixels+registration_disk+stage_ram+256::bigint*256*256+268435456;
 IF p_operation='optical.panorama' AND p_params->>'projection'='cylindrical' THEN
  projection_disk:=25*source_pixels+4*n*4096;
  projection_ram:=16*source_pixels+projection_disk+41*max_source+4*4096+256::bigint*256*256+268435456+4096*max_width;
 END IF;
 encoded_disk:=20*output+3*4096;
 encoding_ram:=16*ref_h*ref_w+16*output+encoded_disk+128*256*256+268435456;
 ram:=downloads+source_ram+proxy_ram+projection_ram+planar_ram+encoding_ram+67108864;
 scratch:=downloads+source_disk+projection_disk+registration_disk+stage_disk+encoded_disk+67108864;
 out_bytes:=encoded_disk+projection_disk+67108864;
 IF ram NOT BETWEEN 1 AND 9007199254740991 OR scratch NOT BETWEEN 1 AND 9007199254740991 OR out_bytes NOT BETWEEN 1 AND 9007199254740991 THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('pixel_count',output,'vram_bytes',0,'ram_bytes',ram,'scratch_bytes',scratch,'output_bytes',out_bytes);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value OR division_by_zero THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_resource_estimate(text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_resource_estimate(text,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) RENAME TO save_v29_multi_draft_pre_resource;
REVOKE ALL ON FUNCTION photo.save_v29_multi_draft_pre_resource(uuid,uuid,uuid,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.save_v29_multi_draft(p_tenant uuid,p_actor uuid,p_session uuid,p_plan text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;expected jsonb;
BEGIN
 IF p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 41943040 THEN RETURN false;END IF;
 plan:=p_plan::jsonb;
 expected:=photo.v29_multi_resource_estimate(plan->>'operation',(plan->>'params_jcs')::jsonb,plan->'sources_jcs');
 IF expected IS NULL OR expected IS DISTINCT FROM plan->'execution_profile'->'resource_estimate'
 OR (plan->>'reserved_bytes')::bigint IS DISTINCT FROM (expected->>'output_bytes')::bigint THEN RETURN false;END IF;
 RETURN photo.save_v29_multi_draft_pre_resource(p_tenant,p_actor,p_session,p_plan);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v29_multi_draft(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
