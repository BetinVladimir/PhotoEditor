-- +goose Up
-- Fenced parent expansion reads normalized metadata evidence, never originals.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_metadata_inventory_entry(p_tenant uuid,p_parent uuid,p_expansion uuid,p_token uuid,
 p_ordinal integer,p_binding jsonb,p_policy text,p_approved text[])
RETURNS TABLE(entry_json text,publication_jcs text,publication_sha text,snapshot_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE raw text;pin text;inventory jsonb;entry jsonb;names text[];publication record;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_token IS NULL OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 OR p_approved IS NULL THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions e JOIN photo.runs r ON r.tenant_id=e.tenant_id AND r.id=e.run_id
 WHERE e.tenant_id=p_tenant AND e.id=p_expansion AND e.run_id=p_parent AND e.kind='v2.delivery/1' AND e.state='open'
 AND e.expansion_token=p_token AND e.expansion_lease_until>clock_timestamp()
 AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested
 AND photo.v30_delivery_expansion_ready(e.id)) THEN RETURN;END IF;
 SELECT r.v30_metadata_inventory_jcs,r.v30_metadata_inventory_sha INTO raw,pin FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_parent;
 IF raw IS NULL OR pin IS DISTINCT FROM encode(sha256(convert_to(raw,'UTF8')),'hex') THEN RETURN;END IF;
 inventory:=raw::jsonb;entry:=inventory->'items'->p_ordinal;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 IF entry IS NULL OR entry->'ordinal' IS DISTINCT FROM to_jsonb(p_ordinal) OR entry->'binding' IS DISTINCT FROM p_binding
 OR inventory->>'parent_run_id' IS DISTINCT FROM p_parent::text OR inventory->>'policy' IS DISTINCT FROM p_policy
 OR inventory->'approved_fields' IS DISTINCT FROM to_jsonb(names) THEN RETURN;END IF;
 SELECT * INTO publication FROM photo.read_v30_metadata_source(p_tenant,p_parent,p_binding,entry->'metadata_source',p_policy,p_approved);
 IF NOT FOUND OR publication.publication_sha IS DISTINCT FROM entry->>'publication_sha256'
 OR (publication.publication_jcs::jsonb)->>'frozen_sha256' IS DISTINCT FROM entry->>'frozen_sha256' THEN RETURN;END IF;
 RETURN QUERY SELECT entry::text,publication.publication_jcs,publication.publication_sha,inventory->>'snapshot_digest';
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_metadata_inventory_entry(uuid,uuid,uuid,uuid,integer,jsonb,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_metadata_inventory_entry(uuid,uuid,uuid,uuid,integer,jsonb,text,text[]) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_metadata_inventory_entry(uuid,uuid,uuid,uuid,integer,jsonb,text,text[]) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
