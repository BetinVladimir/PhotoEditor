-- +goose Up
-- Live original ACL is separate from value validation and publication readiness.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_source_authorized(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_scope jsonb,p_raw text,p_pin text,p_metadata jsonb,p_source text,p_policy text,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR p_session IS NULL
 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_scope->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_scope->>'project_id' IS DISTINCT FROM p_project::text
 OR p_scope->>'actor_id' IS DISTINCT FROM p_actor::text
 OR p_scope->>'session_id' IS DISTINCT FROM p_session::text
 OR photo.v30_metadata_source_publication_valid(p_raw,p_pin,p_scope,p_metadata,p_source,p_policy,p_approved) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v30_delivery_metadata_original(p_tenant,p_project,
 (p_scope->'binding'->>'asset_version_id')::uuid,(p_scope->'binding'->'source'->>'artifact_id')::uuid,
 p_scope->'binding'->'source'->>'sha256',p_actor,p_session) original
 WHERE original.artifact_id=(p_scope->'original'->>'artifact_id')::uuid
 AND original.sha256=p_scope->'original'->>'sha256'
 AND original.bytes=(p_scope->>'original_bytes')::bigint
 AND original.media_type=p_scope->>'original_mime');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_source_authorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_source_authorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[])
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_metadata_source_authorized(uuid,uuid,uuid,uuid,jsonb,text,text,jsonb,text,text,text[]) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
