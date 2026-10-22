-- +goose Up
-- API reads only a scoped boolean. Private scheduler/activation primitives keep
-- their installed ACLs; no capacity reservation or activation occurs here.
-- +goose StatementBegin
CREATE FUNCTION photo.editor_capability_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_operation text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT NULL
 AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_operation IN('editor.effects_seal','editor.layers_precompose')
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND photo.editor_internal_dispatch_installed(p_operation)
 AND EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation=p_operation AND op.version='1' AND op.v2_enabled)
 AND EXISTS(SELECT FROM photo.worker_registrations w
 JOIN photo.resource_nodes n ON n.node_id=w.node_id
 WHERE w.tenant_id=p_tenant AND w.enabled AND w.execution_pool='general'
 AND photo.capacity_policy_worker_fresh(w)
 AND w.resource_classes ? 'cpu' AND w.backends ? 'cpu'
 AND (w.qualified_operations->p_operation) ? 'cpu'
 AND n.enabled AND photo.capacity_policy_node_fresh(n)
 AND photo.worker_authorized(p_tenant,w.worker_id,'cpu',w.node_id,p_operation,'cpu',NULL));
$$;
ALTER FUNCTION photo.editor_capability_ready(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_capability_ready(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.editor_capability_ready(uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
