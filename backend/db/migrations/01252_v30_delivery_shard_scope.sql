-- +goose Up
-- Exact shard authorization before reserve and again before object completion.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_shard_scope(p_expansion uuid,p_token uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;frozen jsonb;body jsonb;request jsonb;observation jsonb;
 expected jsonb;item jsonb;shard integer;first_slot integer;count_slots integer;idx integer;reason text;
BEGIN
 IF p_purpose IS NULL OR p_purpose !~ '^v30_delivery_slots_[0-9]{3}$'
 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 shard:=right(p_purpose,3)::integer;IF shard>=160 THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=e.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL OR e.cursor::integer<>e.expected_count THEN RETURN NULL;END IF;
 frozen:=(payload->'proof'->>'frozen_input_jcs')::jsonb;body:=p_body::jsonb;
 IF frozen->'policy'->>'metadata_policy' IS DISTINCT FROM 'strip'
 OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'off' THEN RETURN NULL;END IF;
 first_slot:=shard*500;count_slots:=least(500,e.expected_count-first_slot);
 IF count_slots<1 OR jsonb_typeof(body->'items') IS DISTINCT FROM 'array'
 OR jsonb_array_length(body->'items')<>count_slots
 OR body-'items' IS DISTINCT FROM jsonb_build_object('schema_version',1,'snapshot_digest',frozen->>'snapshot_digest') THEN RETURN NULL;END IF;
 FOR idx IN 0..count_slots-1 LOOP
  request:=photo.v30_delivery_child_request(p_expansion,p_token,first_slot+idx);
  observation:=photo.v30_delivery_export_observation(p_expansion,p_token,first_slot+idx);
  IF request IS NULL OR observation IS NULL THEN RETURN NULL;END IF;
  expected:=jsonb_build_object('ordinal',(request->>'ordinal')::integer,'binding',request->'binding',
   'preset_id',request->'preset'->>'preset_id','preset_sha',request->'preset'->>'preset_sha',
   'required',(request->'preset'->>'required')::boolean);
  IF observation->>'run_state'='completed' AND observation->>'export_state'='ready' AND (observation->>'artifact_ready')::boolean THEN
   expected:=expected||jsonb_build_object('state','ready','export_id',observation->>'export_id','export',observation->'artifact','reason',NULL);
  ELSE
   IF observation->>'run_state' NOT IN('completed','completed_with_errors','failed','cancelled')
   OR (request->'preset'->>'required')::boolean OR (frozen->'policy'->>'allow_omitted')::boolean IS DISTINCT FROM true THEN RETURN NULL;END IF;
   reason:=CASE WHEN observation->>'run_state'='cancelled' THEN 'EXPORT_CANCELLED'
    WHEN observation->>'run_state'='completed' THEN 'EXPORT_NOT_READY' ELSE 'EXPORT_FAILED' END;
   expected:=expected||jsonb_build_object('state','omitted','export_id',NULL,'export',NULL,'reason',reason);
  END IF;
  item:=body->'items'->idx;IF item IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,
 'owner',photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0'),
 'sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_shard_scope(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_shard_scope(uuid,uuid,text,text) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
