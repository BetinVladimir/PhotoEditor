-- +goose Up
-- Private initial watermark value/source/resource gate. It creates no task.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_initial_stage_scope(p_expansion uuid,p_token uuid,p_slot integer,p_params text,p_image text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;cfg jsonb;params jsonb;image jsonb;mark jsonb;pin record;body jsonb;expected jsonb;
 e photo.graph_expansions%ROWTYPE;w numeric;h numeric;mw numeric;mh numeric;pixels numeric;mark_pixels numeric;channels numeric;owner uuid;profile jsonb;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);IF request IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF e.tenant_id IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 cfg:=(request->>'config_jcs')::jsonb;params:=p_params::jsonb;image:=p_image::jsonb;
 IF jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object' OR cfg ? 'size' OR cfg ? 'sharpen'
 OR encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM request->'binding'->'source'->>'sha256'
 OR image->>'dtype' IS DISTINCT FROM 'float32' OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR image->'color'->>'encoding' IS DISTINCT FROM 'gamma24'
 OR photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO pin FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,(request->'preset'->>'preset_id')::uuid,request->'preset'->>'preset_sha');
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=pin.pin_jcs::jsonb;mark:=(body->>'manifest_jcs')::jsonb;
 IF image->'color' IS DISTINCT FROM mark->'color' THEN RETURN NULL;END IF;
 IF coalesce(image->'canvas'->>'width','') !~ '^[1-9][0-9]{0,5}$' OR coalesce(image->'canvas'->>'height','') !~ '^[1-9][0-9]{0,5}$' THEN RETURN NULL;END IF;
 w:=(image->'canvas'->>'width')::numeric;h:=(image->'canvas'->>'height')::numeric;
 mw:=(mark->'canvas'->>'width')::numeric;mh:=(mark->'canvas'->>'height')::numeric;
 pixels:=w*h;mark_pixels:=mw*mh;
 IF w>100000 OR h>100000 OR pixels NOT BETWEEN 1 AND 200000000 OR mark_pixels NOT BETWEEN 1 AND 200000000 THEN RETURN NULL;END IF;
 expected:=jsonb_build_object('image',request->'binding'->'source','watermark',body->'binding'->'source',
 'opacity',cfg->'watermark'->'opacity','anchor',cfg->'watermark'->'anchor',
 'margin_x',(cfg->'watermark'->>'margin_x')::double precision/w::double precision,
 'margin_y',(cfg->'watermark'->>'margin_y')::double precision/h::double precision,
 'max_width_fraction',0.25,'algorithm','linear_alpha_v1');
 IF params IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 channels:=CASE WHEN image->'alpha' IS NULL OR image->'alpha'='null'::jsonb THEN 12 ELSE 16 END;
 profile:=jsonb_build_object('backend','cpu','renderer_version','v2.2','resource_estimate',jsonb_build_object(
 'ram_bytes',268435456+192*(pixels+mark_pixels),'vram_bytes',0,'scratch_bytes',33554432+channels*pixels+16*mark_pixels,
 'output_bytes',8388608+channels*pixels,'pixel_count',pixels));
 owner:=photo.v30_uuid5(photo.v30_uuid5((request->>'child_run_id')::uuid,'workflow'),'derivative.watermark:0');
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,'owner',owner,'sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'),
 'bytes',octet_length(p_params),'execution_profile',profile);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value OR division_by_zero THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_watermark_initial_stage_scope(uuid,uuid,integer,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_initial_stage_scope(uuid,uuid,integer,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
