-- +goose Up
-- Dedicated Params publication scope cannot bypass watermark producer receipt.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_encode_params_scope(p_expansion uuid,p_token uuid,p_slot integer,p_params text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;b photo.graph_expansions%ROWTYPE;request jsonb;cfg jsonb;params jsonb;expected jsonb;producer jsonb;image_ref jsonb;owner uuid;predecessor uuid;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';IF NOT FOUND THEN RETURN NULL;END IF;
 IF e.tenant_id IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=e.tenant_id FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);IF request IS NULL THEN RETURN NULL;END IF;
 cfg:=(request->>'config_jcs')::jsonb;params:=p_params::jsonb;
 IF jsonb_typeof(cfg->'watermark') IS DISTINCT FROM 'object' OR photo.lock_v30_watermark_stage_source(p_expansion,p_token,p_slot) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN NULL;END IF;
 predecessor:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'derivative.watermark:0');
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND parent_task_id=predecessor AND kind='v30.delivery.watermark_encode/1' FOR UPDATE;
 IF NOT FOUND OR b.state<>'open' OR b.cursor<>'0' OR b.expected_count<>1 THEN RETURN NULL;END IF;
 producer:=photo.v30_watermark_stage_result(p_expansion,p_token,p_slot,'derivative.watermark');
 IF producer IS NULL OR producer->>'task_id' IS DISTINCT FROM predecessor::text
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-watermark-stage-verified' AND event_id=photo.v2_adoption_event_id(predecessor,(producer->>'result_id')::uuid)) THEN RETURN NULL;END IF;
 SELECT value->'ref' INTO image_ref FROM jsonb_array_elements(producer->'outputs') WHERE value->>'role'='working_image';
 IF image_ref IS NULL OR coalesce(params->>'icc_sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 expected:=(cfg-'naming_template'-'watermark'-'sharpen'-'size')||jsonb_build_object('image',image_ref,'icc_sha256',params->>'icc_sha256','metadata_policy','strip');
 IF params IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),'export.encode:0');
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,'owner',owner,'sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'),'bytes',octet_length(p_params));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_watermark_encode_params_scope(uuid,uuid,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_encode_params_scope(uuid,uuid,integer,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
DO $$
DECLARE signature text;old_name text;new_name text;definition text;needle text;
BEGIN
 FOR signature,old_name,new_name IN SELECT * FROM (VALUES
 ('photo.reserve_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,uuid)','reserve_v30_delivery_child_params','reserve_v30_watermark_encode_params'),
 ('photo.complete_v30_delivery_child_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid)','complete_v30_delivery_child_params','complete_v30_watermark_encode_params')
 ) entries LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  needle:='photo.'||old_name||'(';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark Params publication name mismatch';END IF;
  definition:=replace(definition,needle,'photo.'||new_name||'(');
  needle:='photo.v30_delivery_params_scope(';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'watermark Params publication scope mismatch';END IF;
  EXECUTE replace(definition,needle,'photo.v30_watermark_encode_params_scope(');
 END LOOP;
END $$;
ALTER FUNCTION photo.reserve_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.complete_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,uuid),photo.complete_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,uuid),photo.complete_v30_watermark_encode_params(uuid,uuid,integer,text,text,uuid,uuid,text,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
