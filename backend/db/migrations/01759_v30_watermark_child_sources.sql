-- +goose Up
-- Dedicated inventory: inherited prepare inputs plus only the persisted mark.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_child_sources(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_after integer)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;raw text;sha text;pin jsonb;refs jsonb;items jsonb;last_ordinal integer;base integer;
BEGIN
 IF p_after IS NULL OR p_after < -1 OR p_after>1000000 OR p_request IS NULL
 OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 THEN RETURN NULL;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected
 OR jsonb_typeof((expected->>'config_jcs')::jsonb->'watermark') IS DISTINCT FROM 'object' THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion
 AND tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT pin_jcs,pin_sha INTO raw,sha FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,
 (expected->'preset'->>'preset_id')::uuid,expected->'preset'->>'preset_sha');
 IF raw IS NULL OR sha IS NULL THEN RETURN NULL;END IF;
 pin:=raw::jsonb;
 refs:=jsonb_build_array(jsonb_build_object('ref',pin->'binding'->'source','contract','WorkingImage'));
 SELECT refs||coalesce(jsonb_agg(jsonb_build_object('ref',value->'ref','contract','NpyArray')),'[]'::jsonb)
 INTO refs FROM jsonb_array_elements(pin->'arrays');
 IF jsonb_array_length(refs)<>3 THEN RETURN NULL;END IF;
 SELECT coalesce(max(ordinal),-1)+1 INTO base FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=e.parent_task_id;
 IF base>999997 THEN RETURN NULL;END IF;
 -- Refuse missing/expired input objects instead of silently shrinking closure.
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=e.tenant_id AND i.task_id=e.parent_task_id AND
 (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(refs) x LEFT JOIN photo.artifacts a
 ON a.tenant_id=e.tenant_id AND a.id=(x->'ref'->>'artifact_id')::uuid AND a.sha256=x->'ref'->>'sha256'
 WHERE a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())) THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(refs) x JOIN photo.v2_task_inputs i
 ON i.tenant_id=e.tenant_id AND i.task_id=e.parent_task_id AND i.artifact_id=(x->'ref'->>'artifact_id')::uuid
 WHERE i.sha256 IS DISTINCT FROM x->'ref'->>'sha256' OR i.contract IS DISTINCT FROM x->>'contract') THEN RETURN NULL;END IF;
 WITH inventory AS (
 SELECT i.ordinal,i.artifact_id,i.sha256,i.contract FROM photo.v2_task_inputs i
 WHERE i.tenant_id=e.tenant_id AND i.task_id=e.parent_task_id AND i.contract NOT IN('Params','FrozenDeliveryInput','InputSnapshot')
 UNION ALL
 SELECT base+(x.n-1)::integer,(x.value->'ref'->>'artifact_id')::uuid,x.value->'ref'->>'sha256',x.value->>'contract'
 FROM jsonb_array_elements(refs) WITH ORDINALITY x(value,n)
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=e.tenant_id AND i.task_id=e.parent_task_id AND i.artifact_id=(x.value->'ref'->>'artifact_id')::uuid)
 ),page AS (
 SELECT i.*,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type FROM inventory i JOIN photo.artifacts a
 ON a.tenant_id=e.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 WHERE i.ordinal>p_after ORDER BY i.ordinal LIMIT 1000
 ) SELECT coalesce(jsonb_agg(jsonb_build_object('ordinal',ordinal,'ref',jsonb_build_object('artifact_id',artifact_id,'sha256',sha256),
 'contract',contract,'bucket',bucket,'object_key',object_key,'object_version',object_version,'bytes',bytes,'media_type',media_type) ORDER BY ordinal),'[]'::jsonb),max(ordinal)
 INTO items,last_ordinal FROM page;
 -- Repeat current source authority after building the observation.
 IF e.expansion_lease_until<=clock_timestamp() OR NOT EXISTS(SELECT 1 FROM photo.read_v30_watermark_pin(e.tenant_id,e.run_id,e.id,p_token,
 (expected->'preset'->>'preset_id')::uuid,expected->'preset'->>'preset_sha') fresh WHERE fresh.pin_jcs=raw AND fresh.pin_sha=sha) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('items',items,'next',last_ordinal);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_watermark_child_sources(uuid,uuid,integer,text,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_child_sources(uuid,uuid,integer,text,integer) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_child_sources(uuid,uuid,integer,text,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
