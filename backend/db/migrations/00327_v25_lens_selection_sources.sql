-- +goose Up
-- Resolve the entire immutable lens selection only under live editor authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_selection_sources(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(ordinal integer,asset_version_id uuid,source_id uuid,source_sha text,
 source_bytes bigint,source_bucket text,source_key text,source_version text,source_media_type text,
 original_id uuid,original_sha text,original_bytes bigint,original_bucket text,original_key text,
 original_version text,original_media_type text,item_count bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.asset_version_id,source.id,source.sha256,source.bytes,source.bucket,
 source.object_key,source.object_version,source.media_type,original.id,original.sha256,
 original.bytes,original.bucket,original.object_key,original.object_version,original.media_type,s.item_count::bigint
 FROM photo.snapshots s
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 WHERE s.tenant_id=p_tenant AND s.id=p_snapshot
 AND photo.v25_lens_snapshot_ready(p_tenant,p_project,p_snapshot,p_actor,p_session)
 ORDER BY i.ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_selection_sources(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_selection_sources(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_selection_sources(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
