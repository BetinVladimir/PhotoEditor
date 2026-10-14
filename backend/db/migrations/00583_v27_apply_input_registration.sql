-- +goose Up
-- Private registration only. Caller must validate original review/source bytes,
-- qualification and resource/quota authority in the same final transaction.
-- This function never seals a run, changes task readiness or emits dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_apply_inputs(p_tenant uuid,p_project uuid,p_parent uuid,p_child uuid,p_nodes jsonb,p_transport jsonb,p_inventory jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE inventory_row jsonb; transport_row jsonb; node_row jsonb; task_row photo.tasks%ROWTYPE; total integer:=0;
BEGIN
 IF p_parent IS NULL OR p_child IS NULL OR p_parent=p_child
 OR NOT photo.semantic_exposure_inventory_batch_ready(p_nodes,p_transport,p_inventory) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_INVENTORY_INVALID' USING ERRCODE='22023';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_TENANT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_APPLY_PROJECT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id IN(p_parent,p_child) ORDER BY id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child AND kind='apply' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested)
 OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_parent AND status='waiting_review' AND NOT cancel_requested) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_RUN_UNAVAILABLE' USING ERRCODE='23514';END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child ORDER BY id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child)<>jsonb_array_length(p_nodes)
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_child) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_INPUTS_NOT_EMPTY' USING ERRCODE='23514';END IF;
 FOR node_row IN SELECT value FROM jsonb_array_elements(p_nodes) LOOP
  SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child AND id=(node_row->>'id')::uuid;
  IF task_row.id IS NULL OR NOT photo.semantic_apply_input_scope(p_tenant,p_project,task_row.id)
  OR task_row.params_artifact_id::text IS DISTINCT FROM node_row->>'params_artifact_id'
  OR task_row.input_digest IS DISTINCT FROM node_row->>'input_digest'
  OR NOT EXISTS(SELECT 1 FROM photo.semantic_targets target JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id
   JOIN photo.v2_workflows w ON w.tenant_id=i.tenant_id AND w.id=i.workflow_id
   WHERE target.tenant_id=p_tenant AND target.child_run_id=p_child AND target.project_id=p_project AND w.run_id=p_parent
   AND task_row.id=photo.semantic_uuid_v5(p_child,'semantic.edit:'||target.id::text)) THEN
  RAISE EXCEPTION 'SEMANTIC_APPLY_TASK_UNAVAILABLE' USING ERRCODE='23514';END IF;
 END LOOP;
 IF NOT photo.semantic_approved_transport_batch_ready(p_tenant,p_parent,p_nodes,p_transport) THEN
 RAISE EXCEPTION 'SEMANTIC_APPLY_TRANSPORT_UNAVAILABLE' USING ERRCODE='23514';END IF;
 FOR inventory_row IN SELECT value FROM jsonb_array_elements(p_inventory) ORDER BY value->>'task_id' LOOP
  SELECT value INTO transport_row FROM jsonb_array_elements(p_transport) WHERE value->>'task_id'=inventory_row->>'task_id';
  INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
  SELECT p_tenant,p_project,(inventory_row->>'task_id')::uuid,(e.value->'ref'->>'artifact_id')::uuid,e.value->'ref'->>'sha256',e.value->>'contract',(e.ordinality-1)::integer
  FROM jsonb_array_elements(inventory_row->'entries') WITH ORDINALITY e(value,ordinality);
  INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
  SELECT p_tenant,p_project,(inventory_row->>'task_id')::uuid,(e.value->>'artifact_id')::uuid,e.value->>'sha256','InputClosureChunk',1000000+(e.ordinality-1)::integer
  FROM jsonb_array_elements(transport_row->'chunks') WITH ORDINALITY e(value,ordinality);
  INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
  VALUES(p_tenant,p_project,(inventory_row->>'task_id')::uuid,(transport_row->'manifest'->>'artifact_id')::uuid,
   transport_row->'manifest'->>'sha256','InputClosureManifest',1000000+jsonb_array_length(transport_row->'chunks'));
  total:=total+jsonb_array_length(inventory_row->'entries')+jsonb_array_length(transport_row->'chunks')+1;
 END LOOP;
 RETURN total;
END $$;
ALTER FUNCTION photo.register_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_apply_inputs(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
