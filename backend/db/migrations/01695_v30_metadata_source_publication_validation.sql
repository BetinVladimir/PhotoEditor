-- +goose Up
-- Private publication value validation. Live principal/lineage authorization,
-- trusted extraction and durable reservation remain separate obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_source_publication_valid(p_raw text,p_pin text,p_scope jsonb,
 p_metadata jsonb,p_source text,p_policy text,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE envelope jsonb;original jsonb;field text;names text[];
BEGIN
 IF p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576
 OR p_pin IS NULL OR p_pin !~ '^[0-9a-f]{64}$'
 OR encode(sha256(convert_to(p_raw,'UTF8')),'hex')<>p_pin
 OR jsonb_typeof(p_scope) IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(p_scope))<>8 THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['tenant_id','project_id','actor_id','session_id'] LOOP
  IF jsonb_typeof(p_scope->field) IS DISTINCT FROM 'string'
  OR (p_scope->>field)::uuid IS NULL
  OR (p_scope->>field)::uuid='00000000-0000-0000-0000-000000000000'::uuid THEN RETURN false;END IF;
 END LOOP;
 original:=p_scope->'original';
 IF jsonb_typeof(original) IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(original))<>2
 OR jsonb_typeof(original->'artifact_id') IS DISTINCT FROM 'string'
 OR (original->>'artifact_id')::uuid IS NULL
 OR (original->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR original->>'sha256' IS NULL OR original->>'sha256' !~ '^[0-9a-f]{64}$'
 OR original->>'artifact_id' IS NOT DISTINCT FROM p_metadata->>'artifact_id'
 OR original->>'artifact_id' IS NOT DISTINCT FROM p_scope->'binding'->'source'->>'artifact_id'
 OR jsonb_typeof(p_scope->'original_bytes') IS DISTINCT FROM 'number'
 OR p_scope->>'original_bytes' !~ '^[1-9][0-9]*$'
 OR (p_scope->>'original_bytes')::numeric NOT BETWEEN 1 AND 2147483648
 OR p_scope->>'original_mime' IS NULL
 OR p_scope->>'original_mime' NOT IN('image/jpeg','image/png','image/tiff') THEN RETURN false;END IF;
 envelope:=p_raw::jsonb;
 IF jsonb_typeof(envelope) IS DISTINCT FROM 'object'
 OR jsonb_typeof(envelope->'frozen_jcs') IS DISTINCT FROM 'string'
 OR jsonb_typeof(envelope->'frozen_sha256') IS DISTINCT FROM 'string'
 OR NOT photo.v30_metadata_frozen_valid(envelope->>'frozen_jcs',envelope->>'frozen_sha256',
 p_scope->'binding',p_metadata,p_source,p_policy,p_approved) THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 -- Equality with the complete external envelope rejects extra/missing fields,
 -- wrong JSON types and receipt-selected policy, approval or source identity.
 RETURN envelope=jsonb_build_object('schema_version',1,'scope',p_scope,'metadata_source',p_metadata,
 'metadata_jcs',p_source,'policy',p_policy,'approved_fields',to_jsonb(names),
 'frozen_jcs',envelope->>'frozen_jcs','frozen_sha256',envelope->>'frozen_sha256');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_source_publication_valid(text,text,jsonb,jsonb,text,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_source_publication_valid(text,text,jsonb,jsonb,text,text,text[])
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
