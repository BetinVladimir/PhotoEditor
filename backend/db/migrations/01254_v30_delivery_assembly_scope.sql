-- +goose Up
-- Root authorization requires exact immutable shard bytes and current exports.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_assembly_scope(p_expansion uuid,p_token uuid,p_body text,p_shards jsonb) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;frozen jsonb;body jsonb;expected jsonb;
 refs jsonb;ref jsonb;scope jsonb;raw text;purpose text;owner uuid;idx integer;n integer;presets jsonb;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR jsonb_typeof(p_shards) IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 n:=jsonb_array_length(p_shards);IF n NOT BETWEEN 1 AND 160 THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=e.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL OR e.cursor::integer<>e.expected_count OR n<>(e.expected_count+499)/500 THEN RETURN NULL;END IF;
 frozen:=(payload->'proof'->>'frozen_input_jcs')::jsonb;body:=p_body::jsonb;refs:=body->'shards';
 IF jsonb_typeof(refs) IS DISTINCT FROM 'array' OR jsonb_array_length(refs)<>n THEN RETURN NULL;END IF;
 SELECT jsonb_agg(value ORDER BY value->>'preset_id') INTO presets FROM jsonb_array_elements(frozen->'presets');
 expected:=jsonb_build_object('schema_version',1,'snapshot_digest',frozen->>'snapshot_digest',
 'item_count',jsonb_array_length(((payload->'proof'->>'snapshot_jcs')::jsonb)->'items'),
 'slot_count',e.expected_count,'policy',frozen->'policy','presets',presets,'shards',refs);
 IF body IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0');
 FOR idx IN 0..n-1 LOOP
  IF jsonb_typeof(p_shards->idx) IS DISTINCT FROM 'string' THEN RETURN NULL;END IF;
  raw:=p_shards->>idx;purpose:='v30_delivery_slots_'||lpad(idx::text,3,'0');
  scope:=photo.v30_delivery_shard_scope(p_expansion,p_token,purpose,raw);
  IF scope IS NULL THEN RETURN NULL;END IF;
  ref:=refs->idx;
  IF ref IS NULL OR ref-'artifact_id'-'sha256'<>'{}'::jsonb
  OR ref->>'sha256' IS DISTINCT FROM scope->>'sha' THEN RETURN NULL;END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(refs) WITH ORDINALITY x(value,ordinality)
   WHERE ordinality<>idx+1 AND value->>'artifact_id'=ref->>'artifact_id') THEN RETURN NULL;END IF;
  PERFORM d.id FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=e.tenant_id AND d.run_id=e.run_id AND d.owner_id=owner AND d.purpose=purpose
  AND d.state='ready' AND d.artifact_id=(ref->>'artifact_id')::uuid AND d.sha256=ref->>'sha256'
  AND d.bytes=(scope->>'bytes')::bigint AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.object_key='tenants/'||e.tenant_id||'/runs/'||e.run_id||'/documents/'||d.artifact_id
  AND a.media_type='application/json' AND a.output_role=purpose AND a.verified_at IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF d,a;
  IF NOT FOUND THEN RETURN NULL;END IF;
 END LOOP;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('tenant',e.tenant_id,'run',e.run_id,'owner',owner,
 'sha',encode(sha256(convert_to(p_body,'UTF8')),'hex'),'bytes',octet_length(p_body));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_assembly_scope(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_assembly_scope(uuid,uuid,text,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
