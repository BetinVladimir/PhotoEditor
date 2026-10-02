-- SaveEdits may use an inline, immutable identity StyleV2 with no style version.
-- +goose Up
ALTER TABLE photo.apply_builds ALTER COLUMN style_version_id DROP NOT NULL;
GRANT SELECT,UPDATE ON photo.apply_builds TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.apply_inputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,item_id uuid,asset_id uuid,asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,payload jsonb,config_snapshot jsonb,style_artifact_id uuid,style_sha256 text,style_bytes bigint)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.item_id,v.asset_id,i.asset_version_id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,i.payload,r.config_snapshot,sa.id,sa.sha256,sa.bytes
 FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 LEFT JOIN photo.style_versions sv ON sv.tenant_id=b.tenant_id AND sv.id=b.style_version_id AND sv.approval_status='accepted'
 LEFT JOIN photo.artifacts sa ON sa.tenant_id=sv.tenant_id AND sa.id=sv.export_artifact_id AND sa.sha256=sv.config_hash AND sa.state='ready' AND sa.object_version IS NOT NULL
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
 AND b.phase IN ('mask','stats','resolve','color','local','quality')
 AND (b.style_version_id IS NULL OR sa.id IS NOT NULL)
 ORDER BY i.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_inputs(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.apply_inputs(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.apply_inputs(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
