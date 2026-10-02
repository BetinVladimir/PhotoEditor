-- +goose Up
-- Dedicated root scope; generic ready uploads do not qualify.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_recipe_scope(p_tenant uuid,p_project uuid,p_version uuid,p_artifact uuid,p_sha text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.v2_draft_uploads u
 JOIN photo.artifacts recipe ON recipe.tenant_id=u.tenant_id AND recipe.id=u.artifact_id
  AND recipe.sha256=u.sha256 AND recipe.object_version=u.object_version
 JOIN photo.asset_versions v ON v.tenant_id=u.tenant_id AND v.id=u.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
 WHERE u.tenant_id=p_tenant AND u.project_id=p_project AND u.asset_version_id=p_version
 AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.state='ready' AND u.render_registered_at IS NOT NULL
 AND u.render_source_id=v.master_artifact_id AND u.render_source_sha=v.content_hash
 AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified' AND v.review_state='accepted'
 AND recipe.state='ready' AND recipe.verified_at IS NOT NULL AND recipe.output_role='document'
 AND recipe.media_type='application/json' AND recipe.bytes=u.bytes AND recipe.bytes BETWEEN 2 AND 1048576
 AND recipe.object_version NOT IN('','null') AND (recipe.expires_at IS NULL OR recipe.expires_at>clock_timestamp())
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.output_role='working_image'
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
