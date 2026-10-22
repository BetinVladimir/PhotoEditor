-- +goose Up
-- API advisory uses candidate discovery; claim retains its session_user binding.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.editor_capability_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_operation text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT NULL
 AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_operation IN('editor.effects_seal','editor.layers_precompose')
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant
 AND photo.interactive_worker_discoverable(p_tenant,w.worker_id,p_operation,'cpu',NULL));
$$;
ALTER FUNCTION photo.editor_capability_ready(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_capability_ready(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_maintenance,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.editor_capability_ready(uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
