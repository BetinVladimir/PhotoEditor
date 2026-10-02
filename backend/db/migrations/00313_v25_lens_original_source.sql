-- +goose Up
-- Compiler-only catalog provenance; never resolve an arbitrary tenant artifact.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_original_source(p_tenant uuid,p_project uuid,
 p_version uuid,p_source uuid,p_sha text,p_actor uuid,p_session uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,
 object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT original.id,original.sha256,original.bytes,original.bucket,
 original.object_key,original.object_version,original.media_type
 FROM photo.tenants tenant
 JOIN photo.asset_versions v ON v.tenant_id=tenant.id AND v.id=p_version
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 WHERE tenant.id=p_tenant AND tenant.status='active'
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND v.master_artifact_id=p_source AND v.content_hash=p_sha AND source.sha256=p_sha
 AND source.output_role='working_image' AND source.media_type='application/json'
 AND source.state='ready' AND source.verified_at IS NOT NULL
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
 AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_original_source(uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_original_source(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_original_source(uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
