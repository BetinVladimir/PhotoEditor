-- +goose Up
-- Dedicated signed transport receipt. The ordinary v1 assemble transport
-- remains unchanged and cannot mistake an envelope for an off-mode root.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_assembly_transport_ready(p_expansion uuid,p_token uuid,
 p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;x photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.backend_documents%ROWTYPE;ref jsonb;envelope jsonb;assembly jsonb;closure jsonb;
 item jsonb;child jsonb;owner uuid;raw text;base_inventory jsonb;expected_inventory jsonb;
 actual_inventory jsonb;document_bytes bigint;input_count bigint;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 scope:=photo.v30_delivery_assembly_params_scope(p_expansion,p_token,p_params);
 IF scope IS NULL THEN RETURN false;END IF;
 owner:=(scope->>'owner')::uuid;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion AND kind='v2.delivery/1'
  AND state='open' AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();
 IF NOT FOUND OR x.tenant_id::text IS DISTINCT FROM scope->>'tenant'
  OR x.run_id::text IS DISTINCT FROM scope->>'run' THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND workflow_type='delivery';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=x.tenant_id AND run_id=x.run_id AND id=owner FOR SHARE;
 IF w.id IS NULL OR t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
  OR t.node_key<>'v2.delivery.assemble:0' OR t.operation<>'v2.delivery.assemble'
  OR t.operation_version<>'1' OR t.resource_class<>'cpu' OR t.id::text IS DISTINCT FROM p_task->>'id'
  OR t.node_key IS DISTINCT FROM p_task->>'node_key' OR t.operation IS DISTINCT FROM p_task->>'operation'
  OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
  OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
  OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
  OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
  OR (t.execution_profile-'v2_input_manifest') IS DISTINCT FROM p_task->'execution_profile'
  OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=x.tenant_id AND run_id=x.run_id
  AND owner_id=t.id AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF d.id IS NULL OR d.sha256 IS DISTINCT FROM scope->>'sha' OR d.sha256 IS DISTINCT FROM p_task->>'params_sha'
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
   AND i.artifact_id=d.artifact_id AND i.sha256=d.sha256 AND i.contract='Params') THEN RETURN false;END IF;
 ref:=(p_params::jsonb)->'assembly';
 raw:=(SELECT envelope_jcs FROM photo.read_v30_delivery_signed_envelope(p_expansion,p_token,
  (ref->>'artifact_id')::uuid,ref->>'sha256'));
 IF raw IS NULL OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id
  AND i.task_id=t.id AND i.artifact_id=(ref->>'artifact_id')::uuid
  AND i.sha256=ref->>'sha256' AND i.contract='SignedDeliveryAssemblyEnvelope') THEN RETURN false;END IF;
 envelope:=raw::jsonb;
 FOR child IN SELECT value FROM jsonb_build_array(envelope->'assembly',envelope->'signing_closure') LOOP
  IF child IS NULL OR child->>'artifact_id' IS NULL OR child->>'sha256' IS NULL THEN RETURN false;END IF;
 END LOOP;
 ref:=envelope->'assembly';
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
  AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256'
  AND i.contract='DeliveryAssemblyInput') THEN RETURN false;END IF;
 SELECT b.v30_delivery_document_jcs::jsonb INTO assembly FROM photo.backend_documents b
 WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id AND b.owner_id=t.id
  AND b.purpose='v30_delivery_assembly' AND b.artifact_id=(ref->>'artifact_id')::uuid
  AND b.sha256=ref->>'sha256' AND b.state='ready';
 IF assembly IS NULL OR assembly->'policy'->>'provenance' IS DISTINCT FROM 'required' THEN RETURN false;END IF;
 ref:=envelope->'signing_closure';
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
  AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256'
  AND i.contract='SignedDeliveryAssemblyClosure') THEN RETURN false;END IF;
 SELECT b.v30_delivery_document_jcs::jsonb INTO closure FROM photo.backend_documents b
 WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id AND b.owner_id=t.id
  AND b.purpose='v30_delivery_signed_closure' AND b.artifact_id=(ref->>'artifact_id')::uuid
  AND b.sha256=ref->>'sha256' AND b.state='ready';
 IF closure IS NULL OR jsonb_typeof(closure->'entries') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(assembly->'shards') LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
   AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256'
   AND i.contract='DeliverySlotShard') THEN RETURN false;END IF;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(assembly->'presets') LOOP
  ref:=item->'config';
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
   AND i.artifact_id=(ref->>'artifact_id')::uuid AND i.sha256=ref->>'sha256'
   AND i.contract='ExportPresetConfig') THEN RETURN false;END IF;
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(closure->'entries') LOOP
  FOR child IN SELECT value FROM jsonb_each(jsonb_build_object('EncodedExport',item->'export',
   'SignedExport',item->'signed','C2PAManifest',item->'manifest')) LOOP
   IF child IS NULL THEN RETURN false;END IF;
  END LOOP;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
   AND i.artifact_id=(item->'export'->>'artifact_id')::uuid AND i.sha256=item->'export'->>'sha256'
   AND i.contract='EncodedExport')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
   AND i.artifact_id=(item->'signed'->>'artifact_id')::uuid AND i.sha256=item->'signed'->>'sha256'
   AND i.contract='SignedExport')
   OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
   AND i.artifact_id=(item->'manifest'->>'artifact_id')::uuid AND i.sha256=item->'manifest'->>'sha256'
   AND i.contract='C2PAManifest') THEN RETURN false;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
  AND i.artifact_id<>t.params_artifact_id AND NOT EXISTS(SELECT 1 FROM photo.v2_artifact_links l
   WHERE l.tenant_id=i.tenant_id AND l.workflow_id=w.id AND l.artifact_id=i.artifact_id AND l.sha256=i.sha256)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_edges edge JOIN photo.tasks predecessor
  ON predecessor.tenant_id=edge.tenant_id AND predecessor.run_id=edge.run_id
  AND predecessor.id=edge.prerequisite_id WHERE edge.tenant_id=x.tenant_id AND edge.run_id=x.run_id
  AND edge.dependent_id=t.id AND edge.prerequisite_id=x.parent_task_id AND edge.condition='success'
  AND predecessor.state='succeeded' AND NOT predecessor.cancel_requested) THEN RETURN false;END IF;
 -- The signed set extends the independently reconstructed ordinary assembly
 -- inventory. Membership checks above cannot authorize extra or omitted inputs.
 ref:=envelope->'assembly';
 base_inventory:=photo.v30_delivery_assembly_inventory(p_expansion,p_token,
  (ref->>'artifact_id')::uuid,ref->>'sha256');
 IF base_inventory IS NULL THEN RETURN false;END IF;
 WITH all_entries AS (
  SELECT value entry FROM jsonb_array_elements(base_inventory)
  UNION ALL SELECT jsonb_build_object('ref',(p_params::jsonb)->'assembly',
   'contract','SignedDeliveryAssemblyEnvelope')
  UNION ALL SELECT jsonb_build_object('ref',envelope->'signing_closure',
   'contract','SignedDeliveryAssemblyClosure')
  UNION ALL SELECT jsonb_build_object('ref',value->'signed','contract','SignedExport')
   FROM jsonb_array_elements(closure->'entries')
  UNION ALL SELECT jsonb_build_object('ref',value->'manifest','contract','C2PAManifest')
   FROM jsonb_array_elements(closure->'entries')
 ), unique_entries AS (SELECT DISTINCT entry FROM all_entries)
 SELECT jsonb_agg(entry ORDER BY entry->'ref'->>'artifact_id') INTO expected_inventory FROM unique_entries;
 IF expected_inventory IS NULL OR EXISTS(SELECT 1 FROM jsonb_array_elements(expected_inventory) x
  GROUP BY x->'ref'->>'artifact_id' HAVING count(*)<>1) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,
  'sha256',i.sha256),'contract',i.contract) ORDER BY i.artifact_id)
 INTO actual_inventory FROM photo.v2_task_inputs i WHERE i.tenant_id=x.tenant_id AND i.task_id=t.id
  AND i.artifact_id<>t.params_artifact_id;
 IF actual_inventory IS DISTINCT FROM expected_inventory THEN RETURN false;END IF;
 SELECT coalesce(sum(b.bytes),0) INTO document_bytes FROM photo.backend_documents b
 WHERE b.tenant_id=x.tenant_id AND b.run_id=x.run_id AND b.owner_id=t.id AND b.state='ready'
  AND (b.artifact_id=((envelope->'assembly'->>'artifact_id')::uuid)
   OR b.artifact_id=((envelope->'signing_closure'->>'artifact_id')::uuid)
   OR b.artifact_id=(((p_params::jsonb)->'assembly'->>'artifact_id')::uuid)
   OR EXISTS(SELECT 1 FROM jsonb_array_elements(assembly->'shards') shard
    WHERE b.artifact_id=(shard->>'artifact_id')::uuid AND b.sha256=shard->>'sha256'));
 SELECT count(*) INTO input_count FROM photo.v2_task_inputs WHERE tenant_id=x.tenant_id AND task_id=t.id;
 IF photo.v30_delivery_assembly_resources_ready(t.execution_profile,document_bytes,input_count)
  IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=x.id AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp());
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signed_assembly_transport_ready(uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
