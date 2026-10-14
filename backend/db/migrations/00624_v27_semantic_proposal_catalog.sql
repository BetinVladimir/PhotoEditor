-- +goose Up
-- Read-only user-mask proposal preflight. Admission must repeat authority under
-- its transaction locks; object bytes and mask geometry require separate checks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_catalog(p_tenant uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_canvas text,p_mask uuid,p_mask_sha text,p_snapshot uuid)
RETURNS TABLE(project_id uuid,item_count integer)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT asset.project_id,s.item_count FROM photo.asset_versions v
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND original.id=asset.original_artifact_id
 JOIN photo.snapshots s ON s.tenant_id=v.tenant_id AND s.id=p_snapshot
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha
 AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_canvas ~ '^[0-9a-f]{64}$' AND p_mask_sha ~ '^[0-9a-f]{64}$'
 AND v.review_state='accepted' AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND photo.v25_lens_snapshot_ready(p_tenant,asset.project_id,p_snapshot,p_actor,p_session)
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.output_role='working_image'
 AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null')
 AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
 JOIN photo.artifacts mask ON mask.tenant_id=u.tenant_id AND mask.id=u.artifact_id AND mask.sha256=u.sha256
 WHERE u.tenant_id=p_tenant AND u.project_id=asset.project_id AND u.asset_version_id=p_version
 AND u.artifact_id=p_mask AND u.sha256=p_mask_sha AND u.state='ready' AND u.completed_at IS NOT NULL
 AND u.user_mask_binding->'source'=jsonb_build_object('artifact_id',p_source::text,'sha256',p_source_sha)
 AND u.user_mask_binding->>'canvas_hash'=p_canvas
 AND mask.state='ready' AND mask.verified_at IS NOT NULL AND mask.output_role='document'
 AND mask.media_type='application/json' AND mask.bytes BETWEEN 2 AND 1048576
 AND mask.bytes=u.bytes AND mask.bucket=u.bucket AND mask.object_version=u.object_version
 AND mask.object_version IS NOT NULL AND mask.object_version NOT IN('','null')
 AND (mask.expires_at IS NULL OR mask.expires_at>clock_timestamp()));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.semantic_proposal_catalog(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_catalog(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_catalog(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
