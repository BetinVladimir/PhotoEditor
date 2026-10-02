-- +goose Up
-- Read-only preflight; run admission must repeat it under its transaction locks.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_snapshot_ready(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.snapshots s
 WHERE s.tenant_id=p_tenant AND s.id=p_snapshot AND s.kind='v2'
 AND s.principal_type='user' AND s.principal_id=p_actor AND s.expires_at IS NULL
 AND s.item_count BETWEEN 1 AND 5000
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND (SELECT count(*) FROM photo.snapshot_items i WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id)=s.item_count
 AND (SELECT count(DISTINCT i.asset_version_id) FROM photo.snapshot_items i WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id)=s.item_count
 AND (SELECT min(i.ordinal)=0 AND max(i.ordinal)=s.item_count-1 FROM photo.snapshot_items i WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id)
 AND (SELECT count(*) FROM photo.snapshot_items i
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
 AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 WHERE i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.output_role='working_image'
 AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
 AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp()))=s.item_count);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_snapshot_ready(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_snapshot_ready(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_snapshot_ready(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
