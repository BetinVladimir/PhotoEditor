-- +goose Up
-- Private input seal only. Final admission must prove source/review bytes,
-- measured owner costs, quota and activation before queueing the child.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_apply_inputs(p_tenant uuid,p_project uuid,p_parent uuid,p_child uuid,p_params jsonb,p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE node jsonb; transport jsonb; task photo.tasks%ROWTYPE; total integer:=0;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_TENANT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_PROJECT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(p_parent,p_child) ORDER BY id FOR UPDATE;
 IF p_parent IS NULL OR p_child IS NULL OR p_parent=p_child
 OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent AND status='waiting_review' AND NOT cancel_requested)
 OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_RUN_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child ORDER BY id FOR UPDATE;
 IF NOT photo.semantic_apply_registered_inventory_ready(p_tenant,p_project,p_child,p_nodes,p_transport,p_inventory)
 OR NOT photo.semantic_approved_transport_batch_ready(p_tenant,p_parent,p_nodes,p_transport) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_INVENTORY_UNAVAILABLE' USING ERRCODE='23514';END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p_nodes) ORDER BY value->>'id' LOOP
  SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child AND id=(node->>'id')::uuid;
  IF task.id IS NULL OR NOT photo.semantic_apply_input_scope(p_tenant,p_project,task.id)
  OR task.execution_profile IS DISTINCT FROM node->'execution_profile'
  OR task.node_key IS DISTINCT FROM node->>'node_key'
  OR NOT EXISTS(SELECT 1 FROM photo.semantic_targets target JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id
   JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.id=i.workflow_id
   WHERE target.tenant_id=p_tenant AND target.child_run_id=p_child AND w.run_id=p_parent
   AND task.id=photo.semantic_uuid_v5(p_child,'semantic.edit:'||target.id::text)) THEN
  RAISE EXCEPTION 'SEMANTIC_APPLY_TASK_UNAVAILABLE' USING ERRCODE='23514';END IF;
 END LOOP;
 IF NOT photo.semantic_approved_exposure_nodes_ready(p_tenant,p_project,p_child,p_params,p_nodes) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_PROFILE_UNAVAILABLE' USING ERRCODE='23514';END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(p_nodes) ORDER BY value->>'id' LOOP
  SELECT value INTO transport FROM jsonb_array_elements(p_transport) WHERE value->>'task_id'=node->>'id';
  UPDATE photo.tasks SET inputs=jsonb_build_array(transport->'manifest')||(transport->'direct_roots'),
   execution_profile=execution_profile||jsonb_build_object('renderer_version','v2.2',
   'contracts_sha','13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5',
   'v2_input_manifest',transport->'manifest','v2_entry_count',(transport->>'entry_count')::integer,'v2_entries_sha',transport->>'entries_sha')
  WHERE tenant_id=p_tenant AND run_id=p_child AND id=(node->>'id')::uuid;
  total:=total+1;
 END LOOP;
 RETURN total;
END $$;
ALTER FUNCTION photo.seal_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
