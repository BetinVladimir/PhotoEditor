-- +goose Up
-- API receives a boolean scoped proof; private upload metadata is not exposed.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_frozen_draft_scope(p_tenant uuid,p_asset uuid,p_document uuid,
 p_draft uuid,p_revision bigint,p_artifact uuid,p_sha text,p_version uuid,p_source uuid,p_source_sha text,
 p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid,false)
 AND p_revision>0 AND EXISTS(
  SELECT 1 FROM photo.v2_draft_uploads u
  JOIN photo.asset_versions v ON v.tenant_id=u.tenant_id AND v.id=u.asset_version_id
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.project_id=u.project_id
  WHERE u.tenant_id=p_tenant AND a.id=p_asset AND a.deleted_at IS NULL
   AND v.id=p_version AND u.document_id=p_document AND u.artifact_id=p_artifact AND u.sha256=p_sha
   AND u.render_source_id=p_source AND u.render_source_sha=p_source_sha
   AND u.render_draft_document_id=p_draft AND u.render_draft_revision=p_revision
   AND photo.v26_render_recipe_scope(p_tenant,u.project_id,v.id,p_artifact,p_sha,p_actor,p_session)
 )
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_frozen_draft_scope(uuid,uuid,uuid,uuid,bigint,uuid,text,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
