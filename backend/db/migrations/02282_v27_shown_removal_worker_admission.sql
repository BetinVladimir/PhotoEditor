-- +goose Up
-- Separate removal copy admission; does not enable or materialize tasks.
-- Independent JCS reconstruction for the shown-result adoption inventory.
-- Allowed contract names, UUIDs and SHA strings are ASCII, so this exact wire
-- grammar requires no general JSON canonicalizer. No authority/admission here.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_removal_inventory_digest_ready(p_task uuid,p_params uuid,p_params_sha text,p_input_digest text,p_entries_sha text,p_entries jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; ref jsonb; aid uuid; prior uuid; contract_name text; body text:='['; first_item boolean:=true; params_count integer:=0;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid OR p_params IS NULL
 OR p_params='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$' OR coalesce(p_input_digest,'') !~ '^[0-9a-f]{64}$'
 OR coalesce(p_entries_sha,'') !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_entries) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_entries) NOT BETWEEN 1 AND 512 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_entries) LOOP
  ref:=item->'ref';contract_name:=item->>'contract';
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR NOT(item ?& ARRAY['ref','contract'])
  OR (item-'ref'-'contract')<>'{}'::jsonb OR jsonb_typeof(item->'contract') IS DISTINCT FROM 'string'
  OR contract_name NOT IN('Params','SemanticPlan','WorkingImage','EditIntent','TransferCandidate','MaskRef','ResolvedTransform','NpyArray','ApprovedSemanticEdit','RemovalPreviewPresentation','RemovalPreviewNumericalEvidence','EncodedImage')
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR NOT(ref ?& ARRAY['artifact_id','sha256'])
  OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(ref->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
  IF contract_name='Params' THEN
   params_count:=params_count+1;
   IF aid<>p_params OR ref->>'sha256'<>p_params_sha THEN RETURN false;END IF;
  ELSIF aid=p_params THEN RETURN false;END IF;
  IF NOT first_item THEN body:=body||',';END IF;
  body:=body||'{"contract":"'||contract_name||'","ref":{"artifact_id":"'||aid::text||'","sha256":"'||(ref->>'sha256')||'"}}';
  first_item:=false;prior:=aid;
 END LOOP;
 body:=body||']';
 RETURN params_count=1 AND encode(sha256(convert_to(body,'UTF8')),'hex')=p_entries_sha
 AND encode(sha256(convert_to('{"entries":'||body||',"operation":"semantic.adopt_shown_removal","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_removal_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_removal_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure binding only: no ACL, live lease, qualification or approval authority.
-- Atomic admission must obtain receipt/preview Params from locked canonical rows,
-- never substitute client supplied JSON as evidence of photographer acceptance.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_removal_params_bound(p_tenant uuid,p_child uuid,p_target uuid,
 p_preview uuid,p_approved jsonb,p_params jsonb,p_preview_params jsonb,p_receipt jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE k text;v jsonb;ids uuid[]:=ARRAY[]::uuid[];aid uuid;item text;
BEGIN
 IF jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR NOT(p_params ?& ARRAY['schema_version','tenant_id','child_run_id','target_id','approved_edit',
 'preview_run_id','preview_task_id','preview_result_id','preview_attempt','preview_fence',
 'presentation','numerical_evidence','working_image','pixels','model_lock_sha','runtime_lock_sha','device_profile_sha'])
 OR (p_params-ARRAY['schema_version','tenant_id','child_run_id','target_id','approved_edit',
 'preview_run_id','preview_task_id','preview_result_id','preview_attempt','preview_fence',
 'presentation','numerical_evidence','working_image','pixels','model_lock_sha','runtime_lock_sha','device_profile_sha'])<>'{}'::jsonb
 OR p_params->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_params->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_params->>'child_run_id' IS DISTINCT FROM p_child::text
 OR p_params->>'target_id' IS DISTINCT FROM p_target::text
 OR p_params->>'preview_run_id' IS DISTINCT FROM p_preview::text
 OR p_params->'approved_edit' IS DISTINCT FROM p_approved
 OR p_preview_params->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_preview_params->>'target_id' IS DISTINCT FROM p_target::text
 OR p_receipt->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['tenant_id','child_run_id','target_id','preview_run_id','preview_task_id','preview_result_id'] LOOP
  IF jsonb_typeof(p_params->k) IS DISTINCT FROM 'string'
  OR coalesce(p_params->>k,'') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN RETURN false;END IF;
  aid:=(p_params->>k)::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(ids) THEN RETURN false;END IF;
  ids:=array_append(ids,aid);
 END LOOP;
 FOREACH k IN ARRAY ARRAY['approved_edit','presentation','numerical_evidence','working_image','pixels'] LOOP
  v:=p_params->k;
  IF jsonb_typeof(v) IS DISTINCT FROM 'object' OR NOT(v ?& ARRAY['artifact_id','sha256'])
  OR (v-'artifact_id'-'sha256')<>'{}'::jsonb
  OR jsonb_typeof(v->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(v->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(v->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(v->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
  aid:=(v->>'artifact_id')::uuid;
  IF aid='00000000-0000-0000-0000-000000000000'::uuid OR aid=ANY(ids) THEN RETURN false;END IF;
  ids:=array_append(ids,aid);
  IF k<>'approved_edit' AND v IS DISTINCT FROM p_receipt->k THEN RETURN false;END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
  IF jsonb_typeof(p_params->k) IS DISTINCT FROM 'string' OR coalesce(p_params->>k,'') !~ '^[0-9a-f]{64}$'
  OR p_params->k IS DISTINCT FROM p_preview_params->k THEN RETURN false;END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['preview_attempt','preview_fence'] LOOP
  item:=p_params->>k;
  IF jsonb_typeof(p_params->k) IS DISTINCT FROM 'number' OR coalesce(item,'') !~ '^[1-9][0-9]{0,18}$'
  OR item::numeric>(CASE WHEN k='preview_attempt' THEN 2147483647::numeric ELSE 9223372036854775807::numeric END)
  OR p_params->k IS DISTINCT FROM p_receipt->replace(k,'preview_','') THEN RETURN false;END IF;
 END LOOP;
 FOREACH k IN ARRAY ARRAY['plan','intent','candidate','protection'] LOOP
  IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->k->>'artifact_id' THEN RETURN false;END IF;
 END LOOP;
 IF p_approved->>'artifact_id' IS NOT DISTINCT FROM p_preview_params->'source'->'source'->>'artifact_id' THEN RETURN false;END IF;
 RETURN coalesce(p_params->>'preview_task_id'=photo.semantic_uuid_v5(p_preview,'semantic.preview_remove_object:'||p_target::text)::text
 AND p_params->'preview_task_id'=p_receipt->'task_id'
 AND p_params->'preview_result_id'=p_receipt->'result_id',false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_removal_params_bound(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_removal_params_bound(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure bounded transport reconstruction; never grants source or execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_removal_transport_ready(p_child uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;params uuid;entries jsonb;roots jsonb;entry_body text;chunk jsonb;manifest jsonb;
 chunk_sha text;manifest_sha text;required record;n integer;
BEGIN
 IF p_child IS NULL OR p_child='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object'
 OR p_params->>'child_run_id' IS DISTINCT FROM p_child::text
 OR jsonb_typeof(p_params->'target_id') IS DISTINCT FROM 'string'
 OR coalesce(p_params->>'target_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR p_params->>'target_id'='00000000-0000-0000-0000-000000000000' THEN RETURN false;END IF;
 IF NOT(p_node ?& ARRAY['id','node_key','operation','operation_version','resource_class','params_artifact_id','params_sha','input_digest','execution_profile'])
 OR (p_node-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile')<>'{}'::jsonb
 OR p_node->>'operation' IS DISTINCT FROM 'semantic.adopt_shown_removal'
 OR p_node->>'operation_version' IS DISTINCT FROM '1' OR p_node->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_node->>'node_key' IS DISTINCT FROM 'semantic.adopt_shown_removal:'||(p_params->>'target_id')::uuid::text THEN RETURN false;END IF;
 task:=photo.semantic_uuid_v5(p_child,'semantic.adopt_shown_removal:'||(p_params->>'target_id')::uuid::text);
 params:=photo.semantic_uuid_v5(task,'Params');
 IF p_node->>'id' IS DISTINCT FROM task::text OR p_node->>'params_artifact_id' IS DISTINCT FROM params::text
 OR p_transport->>'task_id' IS DISTINCT FROM task::text OR p_inventory->>'task_id' IS DISTINCT FROM task::text
 OR NOT(p_inventory ?& ARRAY['task_id','entries']) OR (p_inventory-'task_id'-'entries')<>'{}'::jsonb
 OR NOT(p_transport ?& ARRAY['task_id','entry_count','entries_sha','manifest','chunks','direct_roots'])
 OR (p_transport-'task_id'-'entry_count'-'entries_sha'-'manifest'-'chunks'-'direct_roots')<>'{}'::jsonb THEN RETURN false;END IF;
 entries:=p_inventory->'entries';
 IF photo.semantic_shown_removal_inventory_digest_ready(task,params,p_node->>'params_sha',
 p_node->>'input_digest',p_transport->>'entries_sha',entries) IS DISTINCT FROM true THEN RETURN false;END IF;
 n:=jsonb_array_length(entries);
 SELECT jsonb_agg(e.value->'ref' ORDER BY e.ordinality),
 '['||string_agg('{"contract":"'||(e.value->>'contract')||'","ref":{"artifact_id":"'||(e.value->'ref'->>'artifact_id')||'","sha256":"'||(e.value->'ref'->>'sha256')||'"}}',',' ORDER BY e.ordinality)||']'
 INTO roots,entry_body FROM jsonb_array_elements(entries) WITH ORDINALITY e(value,ordinality);
 IF p_transport->'direct_roots' IS DISTINCT FROM roots OR p_transport->'entry_count' IS DISTINCT FROM to_jsonb(n)
 OR jsonb_typeof(p_transport->'chunks') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 IF jsonb_array_length(p_transport->'chunks')<>1 THEN RETURN false;END IF;
 chunk_sha:=encode(sha256(convert_to('{"entries":'||entry_body||',"schema_version":1}','UTF8')),'hex');
 chunk:=jsonb_build_object('artifact_id',photo.semantic_uuid_v5(task,'v2_input_chunk_0'),'sha256',chunk_sha);
 IF p_transport->'chunks' IS DISTINCT FROM jsonb_build_array(chunk) THEN RETURN false;END IF;
 manifest_sha:=encode(sha256(convert_to('{"chunks":[{"artifact_id":"'||(chunk->>'artifact_id')||'","sha256":"'||chunk_sha||'"}],"entry_count":'||n::text||',"entries_sha":"'||(p_transport->>'entries_sha')||'","schema_version":1,"task_id":"'||task::text||'"}','UTF8')),'hex');
 manifest:=jsonb_build_object('artifact_id',photo.semantic_uuid_v5(task,'v2_input_manifest'),'sha256',manifest_sha);
 IF p_transport->'manifest' IS DISTINCT FROM manifest THEN RETURN false;END IF;
 FOR required IN SELECT * FROM (VALUES
 (p_params->'approved_edit','ApprovedSemanticEdit'),(p_params->'presentation','RemovalPreviewPresentation'),
 (p_params->'numerical_evidence','RemovalPreviewNumericalEvidence'),(p_params->'working_image','WorkingImage'),
 (p_params->'pixels','NpyArray')) AS required_roots(ref,contract) LOOP
 IF required.ref IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(entries) item
 WHERE item->'ref'=required.ref AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_removal_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_removal_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private persisted closure predicate, usable before activation and at claim.
-- Lease, qualification and current editor authority remain independent gates.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_removal_closure_ready(p_task uuid,p_params jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;entries jsonb;roots jsonb;
 chunks jsonb;manifest jsonb;node jsonb;transport jsonb;inventory jsonb;profile jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind<>'apply' OR r.cancel_requested OR t.cancel_requested
 OR r.status NOT IN('draft','queued','running','completed') OR t.operation<>'semantic.adopt_shown_removal'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5'
 THEN RETURN false;END IF;
 profile:=t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha';
 IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' OR p_params->>'tenant_id' IS DISTINCT FROM t.tenant_id::text
 OR p_params->>'child_run_id' IS DISTINCT FROM r.id::text OR profile->>'backend' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=p_params->>'target_id' AND n->>'params_artifact_id'=t.params_artifact_id::text)<>1 THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id) NOT BETWEEN 3 AND 514
 THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
 'contract',i.contract) ORDER BY i.ordinal),
 jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256) ORDER BY i.ordinal)
 INTO entries,roots FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.ordinal<1000000;
 IF entries IS NULL OR (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id)
 IS DISTINCT FROM jsonb_array_length(entries)+2
 OR EXISTS(SELECT 1 FROM (SELECT ordinal,row_number() OVER(ORDER BY ordinal)-1 AS expected
 FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id AND ordinal<1000000) direct
 WHERE direct.ordinal<>direct.expected)
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
 AND i.project_id::text IS DISTINCT FROM r.config_snapshot->>'project_id') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
 WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND (a.id IS NULL OR a.sha256 IS DISTINCT FROM i.sha256
 OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) ORDER BY ordinal)
 INTO chunks FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id
 AND contract='InputClosureChunk' AND ordinal=1000000;
 SELECT jsonb_build_object('artifact_id',artifact_id,'sha256',sha256) INTO manifest
 FROM photo.v2_task_inputs WHERE tenant_id=t.tenant_id AND task_id=t.id
 AND contract='InputClosureManifest' AND ordinal=1000001;
 node:=jsonb_build_object('id',t.id,'node_key',t.node_key,'operation',t.operation,'operation_version',t.operation_version,
 'resource_class',t.resource_class,'params_artifact_id',t.params_artifact_id,
 'params_sha',(SELECT n->>'params_sha' FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n WHERE n->>'task_id'=t.id::text),'input_digest',t.input_digest,'execution_profile',profile);
 transport:=jsonb_build_object('task_id',t.id,'entry_count',t.execution_profile->'v2_entry_count',
 'entries_sha',t.execution_profile->>'v2_entries_sha','manifest',manifest,'chunks',chunks,'direct_roots',roots);
 inventory:=jsonb_build_object('task_id',t.id,'entries',entries);
 RETURN coalesce(t.inputs=jsonb_build_array(manifest)||roots
 AND t.execution_profile->'v2_input_manifest'=manifest
 AND photo.semantic_shown_removal_transport_ready(r.id,p_params,
 node,transport,inventory) IS TRUE,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_removal_closure_ready(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_removal_closure_ready(uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Separate shown-copy lease admission; independent of proposal revisions. No producer is enabled by this migration.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_semantic_shown_removal(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;s photo.resource_slots%ROWTYPE;
 e jsonb;old_preview photo.runs%ROWTYPE;producer photo.tasks%ROWTYPE;project uuid;actor uuid;session_id uuid;field text;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR encode(public.digest(convert_to(p_body,'UTF8'),'sha256'),'hex')<>p_sha
 OR p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF r.id IS NULL OR r.kind<>'apply' OR r.status NOT IN('queued','running')
 OR r.cancel_requested OR NOT r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 IF t.id IS NULL OR t.operation<>'semantic.adopt_shown_removal' OR t.operation_version<>'1'
 OR t.resource_class<>'cpu' OR t.state<>'running' OR t.cancel_requested
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.params_artifact_id IS DISTINCT FROM p_params
 OR t.input_digest IS DISTINCT FROM p_digest OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF s.id IS NULL OR s.resource_class IS DISTINCT FROM 'cpu' OR s.fencing_token IS DISTINCT FROM p_fence
 OR s.lease_until IS NULL OR s.lease_until<=clock_timestamp()
 OR NOT photo.worker_authorized(p_tenant,p_worker,'cpu',s.node_id,t.operation,'cpu',t.execution_profile->>'model_lock_id') THEN RETURN false;END IF;
 PERFORM worker_id FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker
 AND db_principal=session_user AND enabled AND v2_renderer_versions ? 'v2.2' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params AND sha256=p_sha
 AND state='ready' AND verified_at IS NOT NULL AND media_type='application/json'
 AND bytes=octet_length(p_body) AND object_version IS NOT NULL AND object_version NOT IN('','null')
 AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO old_preview FROM photo.runs WHERE tenant_id=p_tenant AND id=(e->>'preview_run_id')::uuid FOR SHARE;
 IF NOT FOUND OR old_preview.kind IS DISTINCT FROM 'preview' OR NOT old_preview.graph_sealed OR old_preview.created_by IS DISTINCT FROM r.created_by
 OR photo.semantic_shown_removal_params_bound(p_tenant,p_run,(e->>'target_id')::uuid,old_preview.id,
 e->'approved_edit',e,old_preview.config_snapshot->'semantic_removal_preview'->'params',old_preview.v27_removal_preview_receipt) IS DISTINCT FROM true
 OR photo.semantic_shown_removal_closure_ready(p_task,e) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOREACH field IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
 IF e->field IS DISTINCT FROM t.execution_profile->field THEN RETURN false;END IF;
 END LOOP;
 actor:=r.created_by;session_id:=(old_preview.config_snapshot->'semantic_removal_preview'->>'session_id')::uuid;
 SELECT i.project_id INTO project FROM photo.semantic_targets target
 JOIN photo.semantic_intents i ON i.tenant_id=target.tenant_id AND i.id=target.intent_id
 JOIN photo.v2_review_batches b ON b.tenant_id=target.tenant_id AND b.id=target.batch_id
 WHERE target.tenant_id=p_tenant AND target.id=(e->>'target_id')::uuid AND target.state='queued'
 AND target.child_run_id=p_run AND b.child_run_id=p_run AND b.actor_id=actor AND b.action='apply'
 AND b.workflow_id=i.workflow_id AND b.project_id=i.project_id AND i.snapshot_id=r.input_snapshot_id
 AND i.id::text=old_preview.config_snapshot->'semantic_removal_preview'->'params'->>'intent_id';
 IF NOT FOUND OR r.config_snapshot->>'project_id' IS DISTINCT FROM project::text
 OR photo.v2_editor_authorized(p_tenant,project,actor,session_id) IS DISTINCT FROM true
 OR photo.v27_downstream_profile_ready(p_tenant,project,'semantic.adopt_shown_removal','1','cpu',t.execution_profile) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=old_preview.id AND id=(e->>'preview_task_id')::uuid FOR SHARE;
 IF NOT FOUND OR producer.state IS DISTINCT FROM 'succeeded'
 OR producer.canonical_result_id::text IS DISTINCT FROM e->>'preview_result_id'
 OR producer.attempt::text IS DISTINCT FROM e->>'preview_attempt' OR producer.fencing_token::text IS DISTINCT FROM e->>'preview_fence'
 OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.id=producer.canonical_result_id
 AND z.task_id=producer.id AND z.attempt=producer.attempt AND z.fencing_token=producer.fencing_token
 AND z.outputs=jsonb_build_array(e->'presentation')) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=actor AND q.share_id IS NULL AND q.files=0
 AND q.bytes>= (t.execution_profile->'resource_estimate'->>'output_bytes')::bigint AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
 RETURN t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND s.lease_until>clock_timestamp();
EXCEPTION WHEN insufficient_privilege OR invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.admit_semantic_shown_removal(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_semantic_shown_removal(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.admit_semantic_shown_removal(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- Coordinates and complete acceptance policy are exposed only through the
-- live leased shown-copy task. No object IO occurs inside this statement.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_removal_report_evidence(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; p photo.v2_qualification_policies%ROWTYPE; report photo.artifacts%ROWTYPE;
BEGIN
 IF photo.admit_semantic_shown_removal(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT t.execution_profile->'qualification' INTO pin
 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task;
 IF photo.v27_downstream_profile_ready(p_tenant,
 (SELECT (config_snapshot->>'project_id')::uuid FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run),
 'semantic.adopt_shown_removal','1','cpu',
 (SELECT execution_profile FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF pin IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'report'->>'artifact_id')::uuid;
 RETURN jsonb_build_object('pin',pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type));
END $$;
ALTER FUNCTION photo.v27_shown_removal_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_removal_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_shown_removal_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
