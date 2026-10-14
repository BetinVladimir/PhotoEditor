-- +goose Up
-- API reads a bounded saved snapshot through existing private project/session
-- authorization. Do not expose the internal authorization helper to API.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_defect_saved_config(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid)
RETURNS TABLE(config_snapshot jsonb,config_hash text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.config_snapshot,r.config_hash FROM photo.runs r
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND r.created_by=p_actor
 AND r.config_snapshot->>'project_id'=p_project::text
 AND jsonb_typeof(r.config_snapshot->'v27_defect_native_prepare')='object'
 AND r.config_snapshot->'v27_defect_native_prepare'->>'auth_session_id'=p_session::text
 AND octet_length(r.config_snapshot::text) BETWEEN 2 AND 4194304;
$$;
ALTER FUNCTION photo.semantic_native_input_defect_saved_config(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_defect_saved_config(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_defect_saved_config(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
