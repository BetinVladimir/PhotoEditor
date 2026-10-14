-- +goose Up
-- State observation only; no admission, completion or output authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_saved_stage_state(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_run uuid,p_operation text,p_config_sha text)
RETURNS TABLE(task_id uuid,task_state text,run_state text,graph_sealed boolean)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.id,t.state,r.status,r.graph_sealed
 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,p_run,p_operation) original
 JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=original.task_id AND t.run_id=p_run
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE original.config_hash=p_config_sha AND p_config_sha ~ '^[0-9a-f]{64}$'
 AND r.config_hash=original.config_hash AND NOT r.cancel_requested AND NOT t.cancel_requested;
$$;
ALTER FUNCTION photo.semantic_native_input_saved_stage_state(uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_saved_stage_state(uuid,uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_saved_stage_state(uuid,uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
