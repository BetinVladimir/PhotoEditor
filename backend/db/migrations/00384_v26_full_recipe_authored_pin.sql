-- +goose Up
ALTER TABLE photo.v2_draft_uploads ADD COLUMN render_authored_sha text
 CHECK(render_authored_sha IS NULL OR render_authored_sha ~ '^[0-9a-f]{64}$');
ALTER FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text)
 RENAME TO register_v26_render_recipe_source;
REVOKE ALL ON FUNCTION photo.register_v26_render_recipe_source(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC,photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.register_v26_render_recipe(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_sha text,p_source uuid,p_source_sha text,p_raw text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authored text;
BEGIN
 IF NOT photo.register_v26_render_recipe_source(p_tenant,p_version,p_document,p_actor,p_session,
  p_artifact,p_sha,p_source,p_source_sha,p_raw) THEN RETURN false;END IF;
 authored:=(p_raw::jsonb)->>'recipe_sha256';
 UPDATE photo.v2_draft_uploads SET render_authored_sha=authored
 WHERE tenant_id=p_tenant AND document_id=p_document AND artifact_id=p_artifact AND sha256=p_sha
  AND state='ready' AND render_registered_at IS NOT NULL
  AND (render_authored_sha IS NULL OR render_authored_sha=authored);
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v26_render_recipe(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
