-- +goose Up
-- Private exact persisted seal predicate. This is not source-byte authority,
-- qualification, activation or quota admission; final caller retains locks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_apply_sealed_inventory_ready(p_tenant uuid,p_project uuid,p_child uuid,p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.semantic_apply_registered_inventory_ready(p_tenant,p_project,p_child,p_nodes,p_transport,p_inventory)
 OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply' AND status='draft'
 AND NOT graph_sealed AND NOT cancel_requested AND config_snapshot ? 'approved_semantic') THEN RETURN false;END IF;
 RETURN NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_nodes) n
 JOIN jsonb_array_elements(p_transport) transport ON transport->>'task_id'=n->>'id'
 LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_child AND t.id::text=n->>'id'
 WHERE t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.node_key IS DISTINCT FROM n->>'node_key'
 OR t.inputs IS DISTINCT FROM jsonb_build_array(transport->'manifest')||(transport->'direct_roots')
 OR t.execution_profile IS DISTINCT FROM (n->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha','13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5',
 'v2_input_manifest',transport->'manifest','v2_entry_count',(transport->>'entry_count')::integer,'v2_entries_sha',transport->>'entries_sha'));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_apply_sealed_inventory_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_apply_sealed_inventory_ready(uuid,uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC;
ALTER FUNCTION photo.seal_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) RENAME TO seal_semantic_apply_inputs_pre_persisted_check;
REVOKE ALL ON FUNCTION photo.seal_semantic_apply_inputs_pre_persisted_check(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) FROM PUBLIC;
CREATE FUNCTION photo.seal_semantic_apply_inputs(p_tenant uuid,p_project uuid,p_parent uuid,p_child uuid,p_params jsonb,p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sealed integer;
BEGIN
 sealed:=photo.seal_semantic_apply_inputs_pre_persisted_check(p_tenant,p_project,p_parent,p_child,p_params,p_nodes,p_transport,p_inventory);
 IF NOT photo.semantic_apply_sealed_inventory_ready(p_tenant,p_project,p_child,p_nodes,p_transport,p_inventory) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 RETURN sealed;
END $$;
ALTER FUNCTION photo.seal_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
