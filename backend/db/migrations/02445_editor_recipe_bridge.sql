-- +goose Up
CREATE TABLE photo.editor_recipe_bridges (
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id), document_id uuid NOT NULL,
 asset_version_id uuid NOT NULL, actor_id uuid NOT NULL,
 editor_revision bigint NOT NULL CHECK(editor_revision>0),
 override_artifact_id uuid NOT NULL, override_sha text NOT NULL CHECK(override_sha~'^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,document_id),
 FOREIGN KEY(tenant_id,document_id) REFERENCES photo.v2_documents(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,override_artifact_id) REFERENCES photo.artifacts(tenant_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE photo.editor_recipe_bridges ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.editor_recipe_bridges FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.editor_recipe_bridges TO photo_api
 USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.editor_recipe_bridges TO photo_fn_owner USING(true) WITH CHECK(true);
REVOKE ALL ON photo.editor_recipe_bridges FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_gateway;
GRANT SELECT,INSERT ON photo.editor_recipe_bridges TO photo_api;
GRANT SELECT,DELETE ON photo.editor_recipe_bridges TO photo_fn_owner;
-- Existing object-cleanup proof still gates this explicit purge allowlist entry.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text:=' SET CONSTRAINTS ALL DEFERRED;';
BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
 OR position('DELETE FROM photo.editor_recipe_bridges' IN definition)>0 THEN
  RAISE EXCEPTION 'EDITOR_BRIDGE_PURGE_SOURCE_SHAPE';
 END IF;
 EXECUTE replace(definition,needle,needle||' DELETE FROM photo.editor_recipe_bridges WHERE tenant_id=j.tenant_id;');
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
