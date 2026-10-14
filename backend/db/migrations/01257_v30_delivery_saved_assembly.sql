-- +goose Up
-- Restart-safe read of a published root, revalidated against all current slots.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_saved_assembly(p_expansion uuid,p_token uuid,p_artifact uuid,p_sha text) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;d photo.backend_documents%ROWTYPE;body jsonb;ref jsonb;
 owner uuid;raw text;shards jsonb:='[]'::jsonb;idx integer:=0;scope jsonb;
BEGIN
 IF p_artifact IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1';
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=e.tenant_id AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(e.run_id,'workflow'),'v2.delivery.assemble:0');
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id AND b.owner_id=owner AND b.purpose='v30_delivery_assembly'
 AND b.artifact_id=p_artifact AND b.sha256=p_sha AND b.state='ready' AND b.v30_delivery_document_jcs IS NOT NULL
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.output_role=b.purpose
 AND a.object_key='tenants/'||e.tenant_id||'/runs/'||e.run_id||'/documents/'||b.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN NULL;END IF;
 body:=d.v30_delivery_document_jcs::jsonb;
 IF jsonb_typeof(body->'shards') IS DISTINCT FROM 'array' OR jsonb_array_length(body->'shards') NOT BETWEEN 1 AND 160 THEN RETURN NULL;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(body->'shards') LOOP
  SELECT b.v30_delivery_document_jcs INTO raw FROM photo.backend_documents b
  WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id AND b.owner_id=owner
  AND b.purpose='v30_delivery_slots_'||lpad(idx::text,3,'0')
  AND b.artifact_id=(ref->>'artifact_id')::uuid AND b.sha256=ref->>'sha256' AND b.state='ready' FOR SHARE;
  IF raw IS NULL THEN RETURN NULL;END IF;
  shards:=shards||jsonb_build_array(raw);idx:=idx+1;
 END LOOP;
 scope:=photo.v30_delivery_assembly_scope(p_expansion,p_token,d.v30_delivery_document_jcs,shards);
 IF scope IS NULL OR scope->>'sha' IS DISTINCT FROM p_sha THEN RETURN NULL;END IF;
 RETURN d.v30_delivery_document_jcs;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_saved_assembly(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_saved_assembly(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_saved_assembly(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
