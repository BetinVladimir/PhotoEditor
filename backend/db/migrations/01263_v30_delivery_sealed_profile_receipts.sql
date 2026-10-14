-- +goose Up
-- Compare frozen execution intent without the three independently checked seal fields.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_profile_matches(p_actual jsonb,p_expected jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(jsonb_typeof(p_actual)='object' AND jsonb_typeof(p_expected)='object'
 AND p_actual ?& ARRAY['v2_input_manifest','v2_entry_count','v2_entries_sha']
 AND NOT (p_expected ?| ARRAY['v2_input_manifest','v2_entry_count','v2_entries_sha'])
 AND (p_actual-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=p_expected,false)
$$;
ALTER FUNCTION photo.v30_delivery_profile_matches(jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_profile_matches(jsonb,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE OR REPLACE FUNCTION photo.v30_delivery_child_receipt(p_expansion uuid,p_token uuid,p_slot integer,p_request text,p_export uuid,p_task jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE expected jsonb;e photo.graph_expansions%ROWTYPE;child photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 w photo.v2_workflows%ROWTYPE;d photo.delivery_packages%ROWTYPE;artifact_sha text;
BEGIN
 IF p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 2097152 OR p_export IS NULL
 OR p_task IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR pg_column_size(p_task)>65536 THEN RETURN NULL;END IF;
 expected:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF expected IS NULL OR p_request::jsonb IS DISTINCT FROM expected THEN RETURN NULL;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 IF p_slot>=e.cursor::integer THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=e.tenant_id AND run_id=e.run_id;
 SELECT * INTO d FROM photo.delivery_packages WHERE tenant_id=e.tenant_id AND workflow_id=w.id;
 SELECT * INTO child FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(expected->>'child_run_id')::uuid FOR SHARE;
 IF child.id IS NULL OR child.kind<>'v2' OR child.status NOT IN('queued','running','waiting_review','completed','failed','cancelling','cancelled')
 OR child.config_snapshot IS DISTINCT FROM jsonb_build_object('project_id',w.project_id,'v30_delivery_export',expected,'export_id',p_export)
 OR NOT EXISTS(SELECT 1 FROM photo.delivery_items i WHERE i.tenant_id=e.tenant_id AND i.package_id=d.id
 AND i.ordinal=(expected->>'ordinal')::integer AND i.preset_id=(expected->'preset'->>'preset_id')::uuid AND i.export_id=p_export) THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=child.id AND id=(p_task->>'id')::uuid FOR SHARE;
 SELECT sha256 INTO artifact_sha FROM photo.artifacts WHERE tenant_id=e.tenant_id AND id=t.params_artifact_id;
 IF t.id IS NULL OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR p_task IS DISTINCT FROM jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,
 'operation_version',t.operation_version,'resource_class',t.resource_class,'params_artifact_id',t.params_artifact_id,
 'params_sha',artifact_sha,'input_digest',t.input_digest,'execution_profile',t.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') THEN RETURN NULL;END IF;
 -- Slot binding and cursor were committed atomically with initial graph seal.
 -- Current graph_sealed may be false during a later derivative expansion.
 RETURN jsonb_build_object('run_id',child.id,'task_id',t.id,'export_id',p_export,'cursor',e.cursor::integer,'status',child.status,'graph_sealed',child.graph_sealed);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_child_receipt(uuid,uuid,integer,text,uuid,jsonb) TO photo_scheduler;

CREATE OR REPLACE FUNCTION photo.v30_delivery_continuation_transport_ready(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;request jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;
 b photo.graph_expansions%ROWTYPE;d photo.backend_documents%ROWTYPE;child uuid;operation text;required_stage integer;
BEGIN
 IF p_stage IS NULL OR p_stage NOT BETWEEN 1 AND 2 OR p_predecessor IS NULL OR p_task IS NULL THEN RETURN false;END IF;
 scope:=photo.v30_delivery_params_scope(p_expansion,p_token,p_slot,p_params);
 IF scope IS NULL THEN RETURN false;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 child:=(request->>'child_run_id')::uuid;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=child AND kind='v30.delivery.export/1' FOR SHARE;
 IF NOT FOUND OR b.state<>'open' OR b.cursor IS DISTINCT FROM (p_stage-1)::text THEN RETURN false;END IF;
 operation:=p_task->>'operation';
 required_stage:=CASE WHEN operation='derivative.sharpen' OR b.expected_count=1 THEN 1 ELSE 2 END;
 IF p_stage<>required_stage OR operation NOT IN('derivative.sharpen','export.encode') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=child AND id=(scope->>'owner')::uuid FOR SHARE;
 IF NOT FOUND OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM p_task->>'node_key'
 OR t.operation IS DISTINCT FROM operation OR t.operation_version IS DISTINCT FROM p_task->>'operation_version'
 OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF NOT FOUND OR d.sha256 IS DISTINCT FROM scope->>'sha' OR d.sha256 IS DISTINCT FROM p_task->>'params_sha' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_edges edge JOIN photo.tasks predecessor
 ON predecessor.tenant_id=edge.tenant_id AND predecessor.run_id=edge.run_id AND predecessor.id=edge.prerequisite_id
 WHERE edge.tenant_id=e.tenant_id AND edge.run_id=child AND edge.dependent_id=t.id
 AND edge.prerequisite_id=p_predecessor AND edge.condition='success' AND predecessor.state='succeeded'
 AND NOT predecessor.cancel_requested) THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_continuation_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_continuation_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_continuation_transport_ready(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;

CREATE OR REPLACE FUNCTION photo.v30_delivery_continuation_activated(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;cfg jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 b photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;previous photo.tasks%ROWTYPE;d photo.backend_documents%ROWTYPE;
 operation text;previous_operation text;required_stage integer;owner uuid;
BEGIN
 IF p_stage IS NULL OR p_stage NOT BETWEEN 1 AND 2 OR p_predecessor IS NULL OR p_task IS NULL
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 IF request IS NULL THEN RETURN false;END IF;
 cfg:=(request->>'config_jcs')::jsonb;operation:=p_task->>'operation';
 IF operation='derivative.sharpen' THEN
  IF cfg->'size' IS NULL OR cfg->'sharpen' IS NULL THEN RETURN false;END IF;
  required_stage:=1;previous_operation:='derivative.resize';
 ELSIF operation='export.encode' THEN
  IF cfg->'size' IS NULL AND cfg->'sharpen' IS NULL THEN RETURN false;END IF;
  required_stage:=CASE WHEN cfg->'size' IS NOT NULL AND cfg->'sharpen' IS NOT NULL THEN 2 ELSE 1 END;
  previous_operation:=CASE WHEN cfg->'sharpen' IS NOT NULL THEN 'derivative.sharpen' ELSE 'derivative.resize' END;
 ELSE RETURN false;END IF;
 IF p_stage<>required_stage THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR r.kind<>'v2' OR r.config_snapshot->'v30_delivery_export' IS DISTINCT FROM request THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.export/1' FOR SHARE;
 IF NOT FOUND OR b.cursor !~ '^[0-2]$' OR b.cursor::integer<p_stage THEN RETURN false;END IF;
 owner:=photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),operation||':0');
 IF p_predecessor<>photo.v30_uuid5(photo.v30_uuid5(r.id,'workflow'),previous_operation||':0') THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=owner FOR SHARE;
 IF NOT FOUND OR t.state NOT IN('ready','leased','running','succeeded','failed','cancelled','skipped')
 OR t.id::text IS DISTINCT FROM p_task->>'id' OR t.node_key IS DISTINCT FROM operation||':0'
 OR t.node_key IS DISTINCT FROM p_task->>'node_key' OR t.operation IS DISTINCT FROM operation
 OR t.operation_version IS DISTINCT FROM p_task->>'operation_version' OR t.resource_class IS DISTINCT FROM p_task->>'resource_class'
 OR t.params_artifact_id::text IS DISTINCT FROM p_task->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM p_task->>'input_digest'
 OR photo.v30_delivery_profile_matches(t.execution_profile,p_task->'execution_profile') IS DISTINCT FROM true
 OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=t.id
 AND purpose='task_params' AND artifact_id=t.params_artifact_id AND state='ready';
 IF NOT FOUND OR d.sha256 IS DISTINCT FROM p_task->>'params_sha'
 OR d.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_params,'UTF8')),'hex') OR d.bytes<>octet_length(p_params) THEN RETURN false;END IF;
 SELECT * INTO previous FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=p_predecessor;
 IF NOT FOUND OR previous.canonical_result_id IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.inbox WHERE tenant_id=e.tenant_id AND consumer='v2-adopt'
 AND event_id=photo.v2_adoption_event_id(previous.id,previous.canonical_result_id))
 OR NOT EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=e.tenant_id AND run_id=r.id
 AND prerequisite_id=p_predecessor AND dependent_id=t.id AND condition='success') THEN RETURN false;END IF;
 IF operation='export.encode' AND NOT EXISTS(SELECT 1 FROM photo.exports WHERE tenant_id=e.tenant_id
 AND id=(r.config_snapshot->>'export_id')::uuid AND task_id=t.id) THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_continuation_activated(uuid,uuid,integer,integer,uuid,jsonb,text) TO photo_scheduler;

CREATE OR REPLACE FUNCTION photo.v30_delivery_assembly_transport_ready(p_expansion uuid,p_token uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 d photo.backend_documents%ROWTYPE;root jsonb;ref jsonb;shard jsonb;item jsonb;idx integer:=0;
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
