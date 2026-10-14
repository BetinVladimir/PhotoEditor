-- +goose Up
-- Sharpen resource profile uses exact verified resize bytes, not initial canvas.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v30_watermark_sharpen_task(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,p_predecessor uuid,p_task jsonb,p_params text,p_reserved bigint,p_image text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE producer jsonb;image_ref jsonb;profile jsonb;
BEGIN
 IF p_stage IS DISTINCT FROM 1 OR p_task->>'operation' IS DISTINCT FROM 'derivative.sharpen' THEN RETURN false;END IF;
 profile:=photo.v30_watermark_derivative_profile('derivative.sharpen',p_params,p_image);
 IF profile IS NULL OR p_task->'execution_profile' IS DISTINCT FROM profile
 OR p_reserved IS DISTINCT FROM (profile->'resource_estimate'->>'output_bytes')::bigint THEN RETURN false;END IF;
 producer:=photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,'derivative.resize');
 IF producer IS NULL OR producer->>'task_id' IS DISTINCT FROM p_predecessor::text THEN RETURN false;END IF;
 SELECT value->'ref' INTO image_ref FROM jsonb_array_elements(producer->'outputs') WHERE value->>'role'='working_image';
 IF image_ref IS NULL OR image_ref IS DISTINCT FROM p_params::jsonb->'image'
 OR image_ref->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_image,'UTF8')),'hex') THEN RETURN false;END IF;
 RETURN photo.create_v30_watermark_sharpen_task(p_expansion,p_token,p_slot,p_stage,p_predecessor,p_task,p_params,p_reserved);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint,text) TO photo_scheduler;
REVOKE ALL ON FUNCTION photo.create_v30_watermark_sharpen_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
