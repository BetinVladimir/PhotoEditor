-- +goose Up
-- New seals emit current schema; existing sealed documents and digests stay unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_retouch_preview_closure_ready(p_task uuid) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;entries jsonb;roots jsonb;
 chunks jsonb;manifest jsonb;node jsonb;transport jsonb;inventory jsonb;profile jsonb;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF r.id IS NULL OR r.kind<>'preview' OR r.cancel_requested OR t.cancel_requested
 OR r.status NOT IN('draft','queued','running','completed') OR t.operation<>'semantic.preview_retouch_region'
 OR t.operation_version<>'1' OR t.resource_class<>'cpu'
 OR t.execution_profile->>'contracts_sha' IS NULL OR t.execution_profile->>'contracts_sha' NOT IN ('13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5','886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90','7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0')
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
 AND intent.id::text=r.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id'
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
 'params_sha',r.config_snapshot->'semantic_retouch_preview'->>'params_sha','input_digest',t.input_digest,'execution_profile',profile);
 transport:=jsonb_build_object('task_id',t.id,'entry_count',t.execution_profile->'v2_entry_count',
 'entries_sha',t.execution_profile->>'v2_entries_sha','manifest',manifest,'chunks',chunks,'direct_roots',roots);
 inventory:=jsonb_build_object('task_id',t.id,'entries',entries);
 RETURN coalesce(t.inputs=jsonb_build_array(manifest)||roots
 AND t.execution_profile->'v2_input_manifest'=manifest
 AND photo.semantic_retouch_preview_transport_ready(r.id,r.config_snapshot->'semantic_retouch_preview'->'params',
 node,transport,inventory) IS TRUE,false);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_closure_ready(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_closure_ready(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_semantic_retouch_preview_inputs(
 p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_target uuid,p_preview uuid,
 p_body text,p_sha text,p_candidate_body text,p_intent_body text,p_source_body text,
 p_reference_body text,p_mask_body text,p_transform_body text,p_protection_body text,
 p_node jsonb,p_transport jsonb,p_inventory jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE owner uuid;registered integer;sealed_profile jsonb;sealed_inputs jsonb;
BEGIN
 registered:=photo.register_semantic_retouch_preview_inputs(p_tenant,p_intent,p_actor,p_session,p_target,p_preview,
 p_body,p_sha,p_candidate_body,p_intent_body,p_source_body,p_reference_body,p_mask_body,p_transform_body,
 p_protection_body,p_node,p_transport,p_inventory);
 IF registered IS DISTINCT FROM jsonb_array_length(p_inventory->'entries')+2 THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_INPUT_REGISTRATION_INCOMPLETE' USING ERRCODE='23514';END IF;
 owner:=(p_node->>'id')::uuid;
 sealed_inputs:=jsonb_build_array(p_transport->'manifest')||(p_transport->'direct_roots');
 sealed_profile:=(p_node->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha','7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0',
 'v2_input_manifest',p_transport->'manifest','v2_entry_count',(p_transport->>'entry_count')::integer,
 'v2_entries_sha',p_transport->>'entries_sha');
 UPDATE photo.tasks SET inputs=sealed_inputs,execution_profile=sealed_profile
 WHERE tenant_id=p_tenant AND run_id=p_preview AND id=owner
 AND state='blocked' AND attempt=0 AND NOT cancel_requested
 AND inputs='[]'::jsonb AND execution_profile=p_node->'execution_profile';
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_PREVIEW_SEAL_STALE' USING ERRCODE='40001';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_preview AND id=owner
 AND state='blocked' AND attempt=0 AND NOT cancel_requested
 AND inputs=sealed_inputs AND execution_profile=sealed_profile
 AND params_artifact_id=(p_node->>'params_artifact_id')::uuid AND input_digest=p_node->>'input_digest') THEN
 RAISE EXCEPTION 'SEMANTIC_PREVIEW_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 RETURN owner;
END $$;
ALTER FUNCTION photo.seal_semantic_retouch_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_retouch_preview_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,text,text,text,text,text,text,jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_retouch_apply_sealed_inputs_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;row jsonb;node jsonb;t photo.tasks%ROWTYPE;bindings text;config_raw text;n integer;expected jsonb;actual jsonb;
BEGIN
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 n:=jsonb_array_length(p_tasks);
 IF n NOT BETWEEN 1 AND 100 OR n<>(SELECT count(DISTINCT x->'node'->>'id') FROM jsonb_array_elements(p_tasks) x) THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_child;
 IF NOT FOUND OR r.kind IS DISTINCT FROM 'apply' OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.created_by IS DISTINCT FROM p_actor OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child)<>n THEN RETURN false;END IF;
 SELECT '['||string_agg('{"params_artifact_id":"'||(x->>'params_artifact_id')::uuid::text||'","params_sha":"'||(x->>'params_sha')||'","target_id":"'||(x->>'target_id')::uuid::text||'","task_id":"'||(x->'node'->>'id')::uuid::text||'"}',',' ORDER BY (x->'node'->>'id')::uuid)||']'
 INTO bindings FROM jsonb_array_elements(p_tasks) x;
 config_raw:='{"project_id":"'||p_project::text||'","shown_semantic":{"nodes":'||bindings||'}}';
 IF r.config_snapshot IS DISTINCT FROM config_raw::jsonb
 OR r.config_hash IS DISTINCT FROM encode(sha256(convert_to(config_raw,'UTF8')),'hex') THEN RETURN false;END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
 node:=row->'node';
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_child AND id=(node->>'id')::uuid;
 IF NOT FOUND OR t.state IS DISTINCT FROM 'blocked' OR t.attempt<>0 OR t.cancel_requested
 OR t.operation IS DISTINCT FROM 'semantic.adopt_shown_retouch_region' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.node_key IS DISTINCT FROM node->>'node_key'
 OR t.params_artifact_id::text IS DISTINCT FROM row->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM node->>'input_digest'
 OR t.inputs IS DISTINCT FROM jsonb_build_array(row->'transport'->'manifest')||(row->'transport'->'direct_roots')
 OR t.execution_profile IS DISTINCT FROM (node->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha',t.execution_profile->>'contracts_sha',
 'v2_input_manifest',row->'transport'->'manifest','v2_entry_count',(row->'transport'->>'entry_count')::integer,
 'v2_entries_sha',row->'transport'->>'entries_sha')
 OR photo.shown_copy_schema_wire_ready(t.execution_profile->>'contracts_sha',(row->>'params_body')::jsonb) IS DISTINCT FROM true
 OR photo.semantic_shown_retouch_transport_ready(p_child,(row->>'params_body')::jsonb,node,row->'transport',row->'inventory') IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.semantic_targets target JOIN photo.v2_review_batches batch
 ON batch.tenant_id=target.tenant_id AND batch.id=target.batch_id
 WHERE target.tenant_id=p_tenant AND target.id::text=row->>'target_id' AND target.state='queued'
 AND target.child_run_id=p_child AND batch.child_run_id=p_child AND batch.project_id=p_project
 AND batch.actor_id=p_actor AND batch.action='apply') THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract,'ordinal',ordinal) ORDER BY ordinal)
 INTO expected FROM (
 SELECT e.value->'ref' AS ref,e.value->>'contract' AS contract,(e.ordinality-1)::integer AS ordinal
 FROM jsonb_array_elements(row->'inventory'->'entries') WITH ORDINALITY e(value,ordinality)
 UNION ALL SELECT row->'transport'->'chunks'->0,'InputClosureChunk',1000000
 UNION ALL SELECT row->'transport'->'manifest','InputClosureManifest',1000001) entries;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
 'contract',i.contract,'ordinal',i.ordinal) ORDER BY i.ordinal),'[]'::jsonb)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id;
 IF actual IS DISTINCT FROM expected OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
 AND i.task_id=t.id AND i.project_id IS DISTINCT FROM p_project)
 OR photo.semantic_shown_retouch_input_objects_ready(p_tenant,row->'inventory'->'entries') IS DISTINCT FROM true
 OR photo.semantic_shown_retouch_closure_ready(t.id,(row->>'params_body')::jsonb) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_retouch_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_semantic_shown_retouch_apply_inputs(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE registered integer;expected integer;row jsonb;sealed_profile jsonb;sealed_inputs jsonb;sealed integer:=0;
BEGIN
 registered:=photo.register_semantic_shown_retouch_apply_inputs(p_tenant,p_project,p_actor,p_child,p_tasks);
 SELECT sum(jsonb_array_length(x.value->'inventory'->'entries')+2)::integer INTO expected
 FROM jsonb_array_elements(p_tasks) AS x(value);
 IF registered IS DISTINCT FROM expected THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUTS_INCOMPLETE' USING ERRCODE='23514';END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->'node'->>'id' LOOP
 sealed_inputs:=jsonb_build_array(row->'transport'->'manifest')||(row->'transport'->'direct_roots');
 sealed_profile:=(row->'node'->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha','7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0',
 'v2_input_manifest',row->'transport'->'manifest','v2_entry_count',(row->'transport'->>'entry_count')::integer,
 'v2_entries_sha',row->'transport'->>'entries_sha');
 UPDATE photo.tasks SET inputs=sealed_inputs,execution_profile=sealed_profile
 WHERE tenant_id=p_tenant AND run_id=p_child AND id=(row->'node'->>'id')::uuid
 AND state='blocked' AND attempt=0 AND NOT cancel_requested AND inputs='[]'::jsonb
 AND execution_profile=row->'node'->'execution_profile';
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_SEAL_STALE' USING ERRCODE='40001';END IF;
 sealed:=sealed+1;
 END LOOP;
 IF sealed IS DISTINCT FROM jsonb_array_length(p_tasks)
 OR photo.semantic_shown_retouch_apply_sealed_inputs_ready(p_tenant,p_project,p_actor,p_child,p_tasks) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_SEAL_INCOMPLETE' USING ERRCODE='23514';END IF;
 RETURN sealed;
END $$;
ALTER FUNCTION photo.seal_semantic_shown_retouch_apply_inputs(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_semantic_shown_retouch_apply_inputs(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
