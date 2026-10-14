-- +goose Up
-- Dedicated source-manifest-bound Params publication. Existing immutable
-- document identities, quota and retry protocol remain authoritative.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_initial_params_scope(p_expansion uuid,p_token uuid,p_slot integer,p_params text,p_image text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tid uuid;
BEGIN
 SELECT tenant_id INTO tid FROM photo.graph_expansions WHERE id=p_expansion;
 IF tid IS NULL OR tid IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=tid AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tid FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 RETURN photo.v30_watermark_initial_stage_scope(p_expansion,p_token,p_slot,p_params,p_image);
END $$;
ALTER FUNCTION photo.v30_watermark_initial_params_scope(uuid,uuid,integer,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_initial_params_scope(uuid,uuid,integer,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
DO $$
DECLARE signature text;old_name text;new_name text;definition text;needle text;replacement text;
BEGIN
 FOR signature,old_name,new_name IN SELECT * FROM (VALUES
 ('photo.reserve_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)','reserve_v30_delivery_child_params','reserve_v30_watermark_initial_params'),
 ('photo.complete_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)','complete_v30_delivery_child_params','complete_v30_watermark_initial_params')
 ) entries LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  FOR needle,replacement IN SELECT * FROM (VALUES
   ('photo.'||old_name||'(','photo.'||new_name||'('),
   ('p_owner uuid)','p_owner uuid, p_image text)'),
   ('photo.v30_delivery_params_scope(p_expansion, p_token, p_slot, p_params)','photo.v30_watermark_initial_params_scope(p_expansion,p_token,p_slot,p_params,p_image)')
  ) patches LOOP
   -- Function bodies preserve original spacing, unlike the formatted header.
   IF needle LIKE 'photo.v30_delivery_params_scope%' AND strpos(definition,needle)=0 THEN needle:='photo.v30_delivery_params_scope(p_expansion,p_token,p_slot,p_params)';END IF;
   IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_INITIAL_PARAMS_DEFINITION_DRIFT: %',needle;END IF;
   definition:=replace(definition,needle,replacement);
  END LOOP;
  EXECUTE definition;
 END LOOP;
END $$;
ALTER FUNCTION photo.reserve_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.complete_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,uuid,text),photo.complete_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,uuid,text),photo.complete_v30_watermark_initial_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
