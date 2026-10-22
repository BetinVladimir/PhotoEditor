-- +goose Up
-- Admission selects an actually authorized installation; it does not reserve a
-- worker or grant API access to private scheduler predicates. Exact receipts
-- are recovered by the caller before this fresh-only selection is invoked.
-- +goose StatementBegin
-- Discovery deliberately does not call worker_authorized: that claim predicate
-- binds db_principal to session_user. Discovery never grants a claim or lease.
CREATE FUNCTION photo.interactive_worker_discoverable(p_tenant uuid,p_worker uuid,p_operation text,p_backend text,p_lock text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_operation IN('mask.prompt','image.generate','editor.effects_seal','editor.layers_precompose')
 AND EXISTS(SELECT FROM photo.worker_registrations w
 JOIN photo.resource_nodes n ON n.node_id=w.node_id
 JOIN photo.operation_registry op ON op.operation=p_operation AND op.version='1'
 WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker
 AND w.enabled AND w.execution_pool='general'
 AND photo.capacity_policy_worker_fresh(w)
 AND n.enabled AND photo.capacity_policy_node_fresh(n)
 AND w.resource_classes ? op.resource_class
 AND w.backends ? p_backend AND op.backends ? p_backend
 AND (w.qualified_operations->p_operation) ? p_backend
 AND (p_lock IS NULL OR w.lock_id=p_lock)
 AND (op.model_id IS NULL OR w.lock_id~'^[0-9a-f]{64}$')
 AND EXISTS(SELECT FROM photo.tenant_capabilities() c WHERE c.operation=p_operation
 AND c.resource_class=op.resource_class AND c.available_backends ? p_backend)
 AND (p_operation NOT IN('editor.effects_seal','editor.layers_precompose')
 OR (op.v2_enabled AND p_backend='cpu' AND op.resource_class='cpu'
 AND photo.editor_internal_dispatch_installed(p_operation))));
$$;
ALTER FUNCTION photo.interactive_worker_discoverable(uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.interactive_worker_discoverable(uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_maintenance,photo_gateway;
CREATE FUNCTION photo.select_interactive_runtime(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_operation text)
RETURNS TABLE(backend text,lock_id text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT b.backend,w.lock_id
 FROM photo.worker_registrations w
 JOIN photo.resource_nodes n ON n.node_id=w.node_id
 JOIN photo.operation_registry op ON op.operation=p_operation AND op.version='1' AND op.resource_class='gpu'
 CROSS JOIN (VALUES ('cuda'::text,0),('mps'::text,1),('cpu'::text,2)) b(backend,priority)
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND p_operation IN('mask.prompt','image.generate')
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND w.tenant_id=p_tenant AND w.enabled AND w.execution_pool='general'
 AND n.enabled AND photo.capacity_policy_node_fresh(n)
 AND photo.capacity_policy_worker_fresh(w)
 AND w.resource_classes ? op.resource_class
 AND w.backends ? b.backend AND op.backends ? b.backend
 AND (w.qualified_operations->p_operation) ? b.backend
 AND w.lock_id~'^[0-9a-f]{64}$'
 AND EXISTS(SELECT FROM photo.tenant_capabilities() c WHERE c.operation=p_operation
  AND c.resource_class=op.resource_class AND c.available_backends ? b.backend)
 AND photo.interactive_worker_discoverable(p_tenant,w.worker_id,p_operation,b.backend,w.lock_id)
 ORDER BY b.priority,w.worker_id LIMIT 1
$$;
ALTER FUNCTION photo.select_interactive_runtime(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.select_interactive_runtime(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_portal,photo_maintenance,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.select_interactive_runtime(uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
