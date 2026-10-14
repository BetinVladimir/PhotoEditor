-- +goose Up
-- Initial resize/sharpen Params for a persisted watermark preset.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_sharpen_params_scope(p_expansion uuid,p_token uuid,p_slot integer,p_params text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;request jsonb;cfg jsonb;operation text;tid uuid;
BEGIN
 SELECT tenant_id INTO tid FROM photo.graph_expansions WHERE id=p_expansion;
 IF tid IS NULL OR tid IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=tid AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tid FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 scope:=photo.v30_delivery_params_scope(p_expansion,p_token,p_slot,p_params);
 IF scope IS NULL THEN RETURN NULL;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 cfg:=(request->>'config_jcs')::jsonb;
 IF jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object' OR (cfg->'size' IS NULL OR cfg->'sharpen' IS NULL)
 OR photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 operation:='derivative.sharpen';
 IF scope->>'owner' IS DISTINCT FROM photo.v30_uuid5(photo.v30_uuid5((request->>'child_run_id')::uuid,'workflow'),operation||':0')::text THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions b WHERE b.tenant_id=tid AND b.run_id=(request->>'child_run_id')::uuid
 AND b.kind='v30.delivery.export/1' AND b.state='open' AND b.cursor='0' AND b.expected_count=2
 AND b.parent_task_id=photo.v30_uuid5(photo.v30_uuid5(b.run_id,'workflow'),'derivative.resize:0')) THEN RETURN NULL;END IF;
 RETURN scope;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_watermark_sharpen_params_scope(uuid,uuid,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_sharpen_params_scope(uuid,uuid,integer,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE signature text;old_name text;new_name text;definition text;needle text;
BEGIN
 FOR signature,old_name,new_name IN SELECT * FROM (VALUES
 ('photo.reserve_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)','reserve_v30_delivery_child_params','reserve_v30_watermark_sharpen_params'),
 ('photo.complete_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)','complete_v30_delivery_child_params','complete_v30_watermark_sharpen_params')
 ) entries LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  needle:='photo.'||old_name||'(';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark Params publication name mismatch';END IF;
  definition:=replace(definition,needle,'photo.'||new_name||'(');
  needle:='photo.v30_delivery_params_scope(';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark Params publication scope mismatch';END IF;
  EXECUTE replace(definition,needle,'photo.v30_watermark_sharpen_params_scope(');
 END LOOP;
END $$;
ALTER FUNCTION photo.reserve_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.complete_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,uuid),photo.complete_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,uuid),photo.complete_v30_watermark_sharpen_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
