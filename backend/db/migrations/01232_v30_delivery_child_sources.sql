-- +goose Up
-- Inherit only the sealed, adopted prepare task's typed input inventory under
-- current frozen-child/lease authority. No arbitrary artifact lookup is exposed.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_child_sources(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_after integer)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;items jsonb;last_ordinal integer;
BEGIN
 IF p_after IS NULL OR p_after < -1 OR p_after>1000000 OR p_request IS NULL
 OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 THEN RETURN NULL;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 -- Never silently omit a missing, expired or unverified input from the closure.
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=e.tenant_id AND i.task_id=e.parent_task_id AND
 (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.bytes<1
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN NULL;END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ordinal',q.ordinal,'ref',jsonb_build_object('artifact_id',q.artifact_id,'sha256',q.sha256),
 'contract',q.contract,'bucket',q.bucket,'object_key',q.object_key,'object_version',q.object_version,'bytes',q.bytes,'media_type',q.media_type) ORDER BY q.ordinal),'[]'::jsonb),max(q.ordinal)
 INTO items,last_ordinal FROM (
 SELECT i.ordinal,i.artifact_id,i.sha256,i.contract,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=e.tenant_id AND i.task_id=e.parent_task_id AND i.ordinal>p_after
 AND i.contract NOT IN('Params','FrozenDeliveryInput','InputSnapshot') ORDER BY i.ordinal LIMIT 1000) q;
 IF e.expansion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('items',items,'next',last_ordinal);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_child_sources(uuid,uuid,integer,text,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_child_sources(uuid,uuid,integer,text,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_child_sources(uuid,uuid,integer,text,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
