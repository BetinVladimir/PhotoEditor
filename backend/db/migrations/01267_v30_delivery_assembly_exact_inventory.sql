-- +goose Up
-- Complete assembly closure must equal server-derived inventory, not a subset.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_delivery_assembly_transport_ready(p_expansion uuid,p_token uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.backend_documents%ROWTYPE;root jsonb;ref jsonb;shard jsonb;item jsonb;idx integer:=0;document_bytes bigint;input_count bigint;expected_inventory jsonb;actual_inventory jsonb;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 scope:=photo.v30_delivery_assembly_params_scope(p_expansion,p_token,p_params);
 IF scope IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id=(scope->>'owner')::uuid FOR SHARE;
 IF t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation<>'v2.delivery.assemble' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM p_task->>'node_key'
 OR t.operation IS DISTINCT FROM p_task->>'operation' OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
 OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF d.id IS NULL OR d.sha256 IS DISTINCT FROM scope->>'sha' OR d.sha256 IS DISTINCT FROM p_task->>'params_sha'
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id
 AND artifact_id=d.artifact_id AND sha256=d.sha256 AND contract='Params') THEN RETURN false;END IF;
 ref:=(p_params::jsonb)->'assembly';
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id
 AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND contract='DeliveryAssemblyInput') THEN RETURN false;END IF;
 SELECT v30_delivery_document_jcs::jsonb INTO root FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id
 AND owner_id=t.id AND purpose='v30_delivery_assembly' AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND state='ready';
 IF root IS NULL THEN RETURN false;END IF;
 FOR ref IN SELECT value->'config' FROM jsonb_array_elements(root->'presets') LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id
  AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND contract='ExportPresetConfig') THEN RETURN false;END IF;
 END LOOP;
 FOR ref IN SELECT value FROM jsonb_array_elements(root->'shards') LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id
  AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND contract='DeliverySlotShard') THEN RETURN false;END IF;
  SELECT v30_delivery_document_jcs::jsonb INTO shard FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id
  AND owner_id=t.id AND purpose='v30_delivery_slots_'||lpad(idx::text,3,'0') AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' AND state='ready';
  IF shard IS NULL THEN RETURN false;END IF;
  FOR item IN SELECT value FROM jsonb_array_elements(shard->'items') WHERE value->>'state'='ready' LOOP
   ref:=item->'export';
   IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id
   AND artifact_id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256') THEN RETURN false;END IF;
  END LOOP;
  idx:=idx+1;
 END LOOP;
 -- Every non-Params input must be retained by this parent workflow, including
 -- any additional transitive source/config/encode evidence from the resolver.
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=e.tenant_id AND i.task_id=t.id
 AND i.artifact_id<>t.params_artifact_id AND NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l
 WHERE l.tenant_id=i.tenant_id AND l.workflow_id=w.id AND l.artifact_id=i.artifact_id AND l.sha256=i.sha256)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_edges edge JOIN photo.tasks predecessor
 ON predecessor.tenant_id=edge.tenant_id AND predecessor.run_id=edge.run_id AND predecessor.id=edge.prerequisite_id
 WHERE edge.tenant_id=e.tenant_id AND edge.run_id=e.run_id AND edge.dependent_id=t.id
 AND edge.prerequisite_id=e.parent_task_id AND edge.condition='success' AND predecessor.state='succeeded'
 AND NOT predecessor.cancel_requested) THEN RETURN false;END IF;
 SELECT coalesce(sum(b.bytes),0) INTO document_bytes FROM photo.backend_documents b
 WHERE b.tenant_id=e.tenant_id AND b.run_id=e.run_id AND b.owner_id=t.id AND b.state='ready'
 AND (b.purpose='v30_delivery_assembly' AND b.artifact_id=(((p_params::jsonb)->'assembly'->>'artifact_id')::uuid)
 OR EXISTS(SELECT 1 FROM jsonb_array_elements(root->'shards') x WHERE b.artifact_id=(x->>'artifact_id')::uuid AND b.sha256=x->>'sha256'));
 SELECT count(*) INTO input_count FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=t.id;
 IF photo.v30_delivery_assembly_resources_ready(t.execution_profile,document_bytes,input_count) IS DISTINCT FROM true THEN RETURN false;END IF;
 -- Membership alone cannot prove complete transitive closure. Match the
 -- entire registered non-Params inventory to the server-derived frozen set.
 ref:=(p_params::jsonb)->'assembly';
 expected_inventory:=photo.v30_delivery_assembly_inventory(p_expansion,p_token,(ref->>'artifact_id')::uuid,ref->>'sha256');
 IF expected_inventory IS NULL THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),'contract',i.contract) ORDER BY i.artifact_id)
 INTO actual_inventory FROM photo.v2_task_inputs i WHERE i.tenant_id=e.tenant_id AND i.task_id=t.id AND i.artifact_id<>t.params_artifact_id;
 IF actual_inventory IS DISTINCT FROM expected_inventory THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=e.id AND expansion_token=p_token AND expansion_lease_until>clock_timestamp());
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_assembly_transport_ready(uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_assembly_transport_ready(uuid,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_assembly_transport_ready(uuid,uuid,jsonb,text) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
