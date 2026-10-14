-- +goose Up
-- Reuse the existing parent-scoped document reservation/quota/recovery protocol.
-- Only the private source predicate is extended to verified continuation stages.
-- +goose StatementBegin
ALTER FUNCTION photo.v30_delivery_params_scope(uuid,uuid,integer,text) RENAME TO v30_delivery_params_scope_initial;
REVOKE ALL ON FUNCTION photo.v30_delivery_params_scope_initial(uuid,uuid,integer,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
CREATE FUNCTION photo.v30_delivery_params_scope(p_expansion uuid,p_token uuid,p_slot integer,p_params text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;request jsonb;cfg jsonb;params jsonb;expected jsonb;producer jsonb;
 e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;b photo.graph_expansions%ROWTYPE;
 operation text;previous text;initial text;owner uuid;image_ref jsonb;
BEGIN
 scope:=photo.v30_delivery_params_scope_initial(p_expansion,p_token,p_slot,p_params);
 IF scope IS NOT NULL THEN RETURN scope;END IF;
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN NULL;END IF;
 cfg:=(request->>'config_jcs')::jsonb;params:=p_params::jsonb;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.cancel_requested OR r.graph_sealed
 OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN NULL;END IF;
 initial:=CASE WHEN cfg->'size' IS NOT NULL THEN 'derivative.resize' ELSE 'derivative.sharpen' END;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND parent_task_id=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),initial||':0')
 AND kind='v30.delivery.export/1' AND state='open' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF params ? 'algorithm' THEN
  IF cfg->'size' IS NULL OR cfg->'sharpen' IS NULL THEN RETURN NULL;END IF;
  operation:='derivative.sharpen';previous:='derivative.resize';
 ELSE
  operation:='export.encode';
  previous:=CASE WHEN cfg->'sharpen' IS NOT NULL THEN 'derivative.sharpen' ELSE 'derivative.resize' END;
  IF cfg->'size' IS NULL AND cfg->'sharpen' IS NULL THEN RETURN NULL;END IF;
 END IF;
 producer:=photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,previous);
 IF producer IS NULL OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v30-stage-verified'
 AND event_id=photo.v2_adoption_event_id((producer->>'task_id')::uuid,(producer->>'result_id')::uuid)) THEN RETURN NULL;END IF;
 SELECT value->'ref' INTO image_ref FROM jsonb_array_elements(producer->'outputs') WHERE value->>'role'='working_image';
 IF operation='derivative.sharpen' THEN
  expected:=(cfg->'sharpen')||jsonb_build_object('image',image_ref,'max_delta',0.03,'algorithm','luma_unsharp_v1');
 ELSE
  IF coalesce(params->>'icc_sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
  expected:=(cfg-'naming_template'-'watermark'-'sharpen'-'size')||jsonb_build_object('image',image_ref,'icc_sha256',params->>'icc_sha256');
 END IF;
 IF params IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),operation||':0');
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,'owner',owner,
 'sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'),'bytes',octet_length(p_params));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_params_scope(uuid,uuid,integer,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_params_scope(uuid,uuid,integer,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
