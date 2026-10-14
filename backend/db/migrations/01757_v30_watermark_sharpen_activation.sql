-- +goose Up
-- Dedicated sharpen activation and current-watermark-authority replay receipt.
-- +goose StatementBegin
DO $$
DECLARE old_name text;new_name text;definition text;needle text;
BEGIN
 FOR old_name,new_name IN SELECT * FROM (VALUES
 ('activate_v30_delivery_continuation','activate_v30_watermark_sharpen'),
 ('v30_delivery_continuation_activated','v30_watermark_sharpen_activated')
 ) entries LOOP
  definition:=pg_get_functiondef(('photo.'||old_name||'(uuid,uuid,integer,integer,uuid,jsonb,text)')::regprocedure);
  needle:='photo.'||old_name||'(';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_SHARPEN_ACTIVATION_NAME_DRIFT';END IF;
  definition:=replace(definition,needle,'photo.'||new_name||'(');
  needle:='BEGIN';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_SHARPEN_ACTIVATION_BODY_DRIFT';END IF;
  definition:=replace(definition,needle,E'BEGIN\n IF p_stage IS DISTINCT FROM 1 OR p_task->>''operation'' IS DISTINCT FROM ''derivative.sharpen'' THEN RETURN false;END IF;');
  IF old_name='activate_v30_delivery_continuation' THEN
   needle:='photo.v30_delivery_continuation_transport_ready(';
   IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_SHARPEN_ACTIVATION_TRANSPORT_DRIFT';END IF;
   definition:=replace(definition,needle,'photo.v30_watermark_sharpen_transport_ready(');
  ELSE
   needle:=' IF p_stage<>required_stage THEN RETURN false;END IF;';
   IF strpos(definition,needle)=0 THEN RAISE EXCEPTION 'WATERMARK_SHARPEN_ACTIVATION_RECEIPT_DRIFT';END IF;
   definition:=replace(definition,needle,$patch$
 IF jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object'
 OR photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_stage<>required_stage THEN RETURN false;END IF;
$patch$);
  END IF;
  definition:=replace(definition,'IF NOT photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) THEN','IF photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) IS DISTINCT FROM true THEN');
  EXECUTE definition;
 END LOOP;
END $$;
ALTER FUNCTION photo.activate_v30_watermark_sharpen(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v30_watermark_sharpen_activated(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_watermark_sharpen(uuid,uuid,integer,integer,uuid,jsonb,text),photo.v30_watermark_sharpen_activated(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_watermark_sharpen(uuid,uuid,integer,integer,uuid,jsonb,text),photo.v30_watermark_sharpen_activated(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
