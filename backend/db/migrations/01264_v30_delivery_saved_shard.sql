-- +goose Up
-- Bounded per-shard recovery under the root identity and current parent lease.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_saved_shard(p_expansion uuid,p_token uuid,p_root uuid,p_sha text,p_index integer) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE payload jsonb;e photo.graph_expansions%ROWTYPE;d photo.backend_documents%ROWTYPE;
 root jsonb;ref jsonb;raw text;owner uuid;purpose text;scope jsonb;
BEGIN
 IF p_root IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_index IS NULL OR p_index NOT BETWEEN 0 AND 159 THEN RETURN NULL;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);
 IF payload IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF e.cursor::integer<>e.expected_count THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0');
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id AND b.owner_id=owner AND b.purpose='v30_delivery_assembly'
 AND b.artifact_id=p_root AND b.sha256=p_sha AND b.state='ready' AND b.v30_delivery_document_jcs IS NOT NULL
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.output_role=b.purpose
 AND a.object_key='tenants/'||e.tenant_id||'/runs/'||e.run_id||'/documents/'||b.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 root:=d.v30_delivery_document_jcs::jsonb;
 IF jsonb_typeof(root->'shards') IS DISTINCT FROM 'array' OR jsonb_array_length(root->'shards')<>(e.expected_count+499)/500
 OR p_index>=jsonb_array_length(root->'shards') THEN RETURN NULL;END IF;
 ref:=root->'shards'->p_index;purpose:='v30_delivery_slots_'||lpad(p_index::text,3,'0');
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id AND b.owner_id=owner AND b.purpose=purpose
 AND b.artifact_id=(ref->>'artifact_id')::uuid AND b.sha256=ref->>'sha256' AND b.state='ready' AND b.v30_delivery_document_jcs IS NOT NULL
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.output_role=b.purpose
 AND a.object_key='tenants/'||e.tenant_id||'/runs/'||e.run_id||'/documents/'||b.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 raw:=d.v30_delivery_document_jcs;
 scope:=photo.v30_delivery_shard_scope(p_expansion,p_token,purpose,raw);
 IF scope IS NULL OR scope->>'sha' IS DISTINCT FROM d.sha256 THEN RETURN NULL;END IF;
 RETURN raw;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_saved_shard(uuid,uuid,uuid,text,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_saved_shard(uuid,uuid,uuid,text,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_saved_shard(uuid,uuid,uuid,text,integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
