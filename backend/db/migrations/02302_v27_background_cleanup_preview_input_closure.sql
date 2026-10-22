-- +goose Up
-- Dedicated background_cleanup transport/digest/closure predicates; no execution authority.
-- Reuses the existing run profile column with a separate background_cleanup config namespace.
-- Independent JCS reconstruction for the bounded independent preview inventory.
-- Allowed contract names, UUIDs and SHA strings are ASCII, so this exact wire
-- grammar requires no general JSON canonicalizer. No authority/admission here.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_inventory_digest_ready(p_task uuid,p_params uuid,p_params_sha text,p_input_digest text,p_entries_sha text,p_entries jsonb) RETURNS boolean
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
  OR contract_name NOT IN('Params','SemanticPlan','WorkingImage','EditIntent','TransferCandidate','MaskRef','ResolvedTransform','NpyArray')
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
 AND encode(sha256(convert_to('{"entries":'||body||',"operation":"semantic.preview_background_cleanup","task":"'||p_task::text||'","version":"1"}','UTF8')),'hex')=p_input_digest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_inventory_digest_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Pure bounded transport reconstruction; never grants source or execution authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_transport_ready(p_preview uuid,p_params jsonb,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task uuid;params uuid;entries jsonb;roots jsonb;entry_body text;chunk jsonb;manifest jsonb;
 chunk_sha text;manifest_sha text;required record;n integer;
BEGIN
 IF p_preview IS NULL OR p_preview='00000000-0000-0000-0000-000000000000'::uuid
 OR jsonb_typeof(p_params) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_node) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF NOT(p_node ?& ARRAY['id','node_key','operation','operation_version','resource_class','params_artifact_id','params_sha','input_digest','execution_profile'])
 OR (p_node-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile')<>'{}'::jsonb
 OR p_node->>'operation' IS DISTINCT FROM 'semantic.preview_background_cleanup'
 OR p_node->>'operation_version' IS DISTINCT FROM '1' OR p_node->>'resource_class' IS DISTINCT FROM 'gpu'
 OR p_node->>'node_key' IS DISTINCT FROM 'semantic.preview_background_cleanup:'||(p_params->>'target_id')::uuid::text THEN RETURN false;END IF;
 task:=photo.semantic_uuid_v5(p_preview,'semantic.preview_background_cleanup:'||(p_params->>'target_id')::uuid::text);
 params:=photo.semantic_uuid_v5(task,'Params');
 IF p_node->>'id' IS DISTINCT FROM task::text OR p_node->>'params_artifact_id' IS DISTINCT FROM params::text
 OR p_transport->>'task_id' IS DISTINCT FROM task::text OR p_inventory->>'task_id' IS DISTINCT FROM task::text
 OR NOT(p_inventory ?& ARRAY['task_id','entries']) OR (p_inventory-'task_id'-'entries')<>'{}'::jsonb
 OR NOT(p_transport ?& ARRAY['task_id','entry_count','entries_sha','manifest','chunks','direct_roots'])
 OR (p_transport-'task_id'-'entry_count'-'entries_sha'-'manifest'-'chunks'-'direct_roots')<>'{}'::jsonb THEN RETURN false;END IF;
 entries:=p_inventory->'entries';
 IF photo.semantic_background_cleanup_preview_inventory_digest_ready(task,params,p_node->>'params_sha',
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
 (p_params->'plan','SemanticPlan'),(p_params->'intent','EditIntent'),
 (p_params->'candidate','TransferCandidate'),(p_params->'source'->'source','WorkingImage'),
 (p_params->'protection','MaskRef')) AS required_roots(ref,contract) LOOP
 IF required.ref IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(entries) item
 WHERE item->'ref'=required.ref AND item->>'contract'=required.contract) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_transport_ready(uuid,jsonb,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Private persisted closure predicate, usable before activation and at claim.
-- Lease, qualification and current editor authority remain independent gates.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_background_cleanup_preview_closure_ready(p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;entries jsonb;roots jsonb;
 chunks jsonb;manifest jsonb;node jsonb;transport jsonb;inventory jsonb;profile jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind<>'preview' OR r.cancel_requested OR t.cancel_requested
 OR r.status NOT IN('draft','queued','running','completed') OR t.operation<>'semantic.preview_background_cleanup'
 OR t.operation_version<>'1' OR t.resource_class<>'gpu'
 OR t.execution_profile->>'contracts_sha' IS DISTINCT FROM '13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5'
 THEN RETURN false;END IF;
 profile:=t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha';
 IF profile IS DISTINCT FROM r.v27_preapply_preview_profile THEN RETURN false;END IF;
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
 AND NOT EXISTS(SELECT 1 FROM photo.semantic_intents intent WHERE intent.tenant_id=t.tenant_id
 AND intent.id::text=r.config_snapshot->'semantic_background_cleanup_preview'->'params'->>'intent_id'
 AND intent.project_id=i.project_id)) THEN RETURN false;END IF;
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
 'params_sha',r.config_snapshot->'semantic_background_cleanup_preview'->>'params_sha','input_digest',t.input_digest,'execution_profile',profile);
 transport:=jsonb_build_object('task_id',t.id,'entry_count',t.execution_profile->'v2_entry_count',
 'entries_sha',t.execution_profile->>'v2_entries_sha','manifest',manifest,'chunks',chunks,'direct_roots',roots);
 inventory:=jsonb_build_object('task_id',t.id,'entries',entries);
 RETURN coalesce(t.inputs=jsonb_build_array(manifest)||roots
 AND t.execution_profile->'v2_input_manifest'=manifest
 AND photo.semantic_background_cleanup_preview_transport_ready(r.id,r.config_snapshot->'semantic_background_cleanup_preview'->'params',
 node,transport,inventory) IS TRUE,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_background_cleanup_preview_closure_ready(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_background_cleanup_preview_closure_ready(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
