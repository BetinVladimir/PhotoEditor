-- +goose Up
-- Independent SQL resource profile, derived from exact source manifest bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_derivative_profile(p_operation text,p_params text,p_image text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE image jsonb;params jsonb;w bigint;h bigint;tw bigint;th bigint;source bigint;target bigint;channels bigint;scale double precision;x double precision;y double precision;
BEGIN
 IF p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 image:=p_image::jsonb;params:=p_params::jsonb;
 IF image->>'dtype' IS DISTINCT FROM 'float32' OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR params->'image'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_image,'UTF8')),'hex') THEN RETURN NULL;END IF;
 w:=(image->'canvas'->>'width')::bigint;h:=(image->'canvas'->>'height')::bigint;
 IF w IS NULL OR h IS NULL OR w NOT BETWEEN 1 AND 100000 OR h NOT BETWEEN 1 AND 100000 OR w*h>200000000 THEN RETURN NULL;END IF;
 tw:=w;th:=h;
 IF p_operation='derivative.resize' THEN
  IF params->>'fit' IS DISTINCT FROM 'contain' OR (params ? 'long_edge')=(params ? 'width' OR params ? 'height')
  OR (params ? 'width')<>(params ? 'height') OR jsonb_typeof(params->'allow_upscale') IS DISTINCT FROM 'boolean' THEN RETURN NULL;END IF;
  IF params ? 'long_edge' THEN
   IF (params->>'long_edge')::bigint NOT BETWEEN 1 AND 100000 THEN RETURN NULL;END IF;
   scale:=(params->>'long_edge')::double precision/greatest(w,h)::double precision;
  ELSE
   IF (params->>'width')::bigint NOT BETWEEN 1 AND 100000 OR (params->>'height')::bigint NOT BETWEEN 1 AND 100000 THEN RETURN NULL;END IF;
   scale:=least((params->>'width')::double precision/w::double precision,(params->>'height')::double precision/h::double precision);
  END IF;
  IF NOT (params->>'allow_upscale')::boolean THEN scale:=least(1::double precision,scale);END IF;
  x:=w::double precision*scale;y:=h::double precision*scale;
  -- Match Go RoundToEven using the same binary64 products.
  tw:=greatest(1,(CASE WHEN x-floor(x)<0.5 THEN floor(x) WHEN x-floor(x)>0.5 THEN floor(x)+1 WHEN mod(floor(x)::bigint,2)=0 THEN floor(x) ELSE floor(x)+1 END)::bigint);
  th:=greatest(1,(CASE WHEN y-floor(y)<0.5 THEN floor(y) WHEN y-floor(y)>0.5 THEN floor(y)+1 WHEN mod(floor(y)::bigint,2)=0 THEN floor(y) ELSE floor(y)+1 END)::bigint);
 ELSIF p_operation IS DISTINCT FROM 'derivative.sharpen' THEN RETURN NULL;END IF;
 source:=w*h;target:=tw*th;
 IF target NOT BETWEEN 1 AND 200000000 THEN RETURN NULL;END IF;
 channels:=CASE WHEN image->'alpha' IS NULL OR image->'alpha'='null'::jsonb THEN 12 ELSE 16 END;
 RETURN jsonb_build_object('backend','cpu','renderer_version','v2.2','resource_estimate',jsonb_build_object(
 'ram_bytes',268435456::bigint+192*(source+target),'vram_bytes',0,
 'scratch_bytes',33554432::bigint+channels*(source+target),'output_bytes',8388608::bigint+channels*target,'pixel_count',target));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_watermark_derivative_profile(text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_derivative_profile(text,text,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;

CREATE FUNCTION photo.create_v30_watermark_derivative_initial_draft(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_export uuid,p_config text,p_task jsonb,p_params text,p_reserved bigint,p_image text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;profile jsonb;
BEGIN
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected
 OR expected->'binding'->'source' IS DISTINCT FROM p_params::jsonb->'image'
 OR expected->'binding'->'source'->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_image,'UTF8')),'hex') THEN RETURN false;END IF;
 profile:=photo.v30_watermark_derivative_profile(p_task->>'operation',p_params,p_image);
 IF profile IS NULL OR p_task->'execution_profile' IS DISTINCT FROM profile THEN RETURN false;END IF;
 RETURN photo.create_v30_watermark_derivative_initial_draft(p_expansion,p_token,p_slot,p_request,p_export,p_config,p_task,p_params,p_reserved);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint,text) TO photo_scheduler;
-- Old core remains callable only by definer wrapper, closing resource bypass.
REVOKE ALL ON FUNCTION photo.create_v30_watermark_derivative_initial_draft(uuid,uuid,integer,text,uuid,text,jsonb,text,bigint) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
