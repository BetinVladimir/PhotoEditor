-- +goose Up
-- Read-only locators for dedicated all-native recovery; never admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_targets_transfer_saved_config(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_request_sha text,p_snapshot uuid)
RETURNS TABLE(config_snapshot jsonb,config_hash text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.config_snapshot,r.config_hash FROM photo.runs r
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND r.created_by=p_actor
 AND r.input_snapshot_id=p_snapshot
 AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_snapshot->>'input_snapshot_id'=p_snapshot::text
 AND jsonb_typeof(r.config_snapshot->'v27_semantic')='object'
 AND r.config_snapshot->'v27_semantic'->>'auth_session_id'=p_session::text
 AND jsonb_typeof(r.config_snapshot->'v27_native_targets_transfer')='object'
 AND r.config_snapshot->'v27_native_targets_transfer'->'schema_version'='1'::jsonb
 AND r.config_snapshot->'v27_native_targets_transfer'->>'parent_run_id'=p_run::text
 AND r.config_snapshot->'v27_native_targets_transfer'->>'request_sha'=p_request_sha
 AND p_request_sha ~ '^[0-9a-f]{64}$' AND r.config_hash ~ '^[0-9a-f]{64}$'
 AND octet_length(r.config_snapshot::text) BETWEEN 2 AND 8388608;
$$;
ALTER FUNCTION photo.semantic_native_targets_transfer_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_targets_transfer_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_targets_transfer_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
