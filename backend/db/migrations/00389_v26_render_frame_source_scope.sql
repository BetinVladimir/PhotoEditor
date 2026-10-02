-- +goose Up
-- Source authority requires the accepted descriptor context and exact snapshot frame.
-- JSON child-ref/canvas validation remains mandatory in the trusted resolver.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_frame_source_scope(
 p_tenant uuid,p_project uuid,p_version uuid,p_root uuid,p_root_sha text,
 p_descriptor uuid,p_descriptor_sha text,p_analysis uuid,p_selected_ordinal integer,p_model_lock text,
 p_frame_ordinal integer,p_frame_version uuid,p_source uuid,p_source_sha text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_frame_ordinal BETWEEN 0 AND 4999
 AND photo.v26_render_descriptor_scope(p_tenant,p_project,p_version,p_root,p_root_sha,
 p_descriptor,p_descriptor_sha,p_analysis,p_selected_ordinal,p_model_lock,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.session_analyses sa
 JOIN photo.snapshot_items item ON item.tenant_id=sa.tenant_id AND item.snapshot_id=sa.snapshot_id
 JOIN photo.asset_versions v ON v.tenant_id=item.tenant_id AND v.id=item.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id
 WHERE sa.tenant_id=p_tenant AND sa.project_id=p_project AND sa.id=p_analysis
 AND item.ordinal=p_frame_ordinal AND item.asset_version_id=p_frame_version
 AND item.artifact_id=p_source AND item.sha256=p_source_sha
 AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha AND v.review_state='accepted'
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND source.sha256=p_source_sha AND source.output_role='working_image' AND source.media_type='application/json'
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.object_version IS NOT NULL
 AND source.object_version NOT IN('','null') AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.scene_groups g JOIN photo.scene_members member
 ON member.tenant_id=g.tenant_id AND member.project_id=g.project_id AND member.group_id=g.id
 WHERE g.tenant_id=sa.tenant_id AND g.project_id=sa.project_id AND g.analysis_id=sa.id
 AND member.asset_version_id=p_frame_version AND member.snapshot_ordinal=p_frame_ordinal)),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_frame_source_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_frame_source_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_frame_source_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,integer,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
