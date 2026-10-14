-- +goose Up
ALTER FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid)
 RENAME TO v26_render_recipe_scope_before_background;
REVOKE ALL ON FUNCTION photo.v26_render_recipe_scope_before_background(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC,photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_recipe_scope(p_tenant uuid,p_project uuid,p_version uuid,p_artifact uuid,p_sha text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE background jsonb;
BEGIN
 IF NOT photo.v26_render_recipe_scope_before_background(p_tenant,p_project,p_version,p_artifact,p_sha,p_actor,p_session) THEN RETURN false;END IF;
 SELECT u.render_background_asset INTO background FROM photo.v2_draft_uploads u
 WHERE u.tenant_id=p_tenant AND u.project_id=p_project AND u.asset_version_id=p_version
  AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.state='ready' AND u.render_registered_at IS NOT NULL
 FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN background IS NULL OR photo.v26_render_background_binding_ready(p_tenant,p_project,background);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
