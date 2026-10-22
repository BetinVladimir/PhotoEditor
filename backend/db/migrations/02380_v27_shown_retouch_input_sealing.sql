-- +goose Up
-- Retouch input scope plus complete registration/seal; all prior guard scopes retained.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_retouch_apply_input_scope(p_tenant uuid,p_project uuid,p_task uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_review_batches b ON b.tenant_id=r.tenant_id AND b.child_run_id=r.id AND b.action='apply'
 JOIN photo.v2_workflows w ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id
 JOIN photo.semantic_targets target ON target.tenant_id=b.tenant_id AND target.batch_id=b.id AND target.child_run_id=r.id
 JOIN photo.projects project ON project.tenant_id=b.tenant_id AND project.id=b.project_id AND project.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
 JOIN photo.backend_documents d ON d.tenant_id=w.tenant_id AND d.run_id=w.run_id AND d.owner_id=t.id
 AND d.purpose='task_params' AND d.state='ready' AND d.artifact_id=params.id
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND b.project_id=p_project AND w.project_id=p_project
 AND target.project_id=p_project AND target.state='queued' AND b.actor_id=r.created_by
 AND r.kind='apply' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot->>'project_id'=p_project::text
 AND jsonb_typeof(r.config_snapshot->'shown_semantic'->'nodes')='array'
 AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested AND t.inputs='[]'::jsonb
 AND NOT(t.execution_profile ? 'v2_input_manifest')
 AND t.operation='semantic.adopt_shown_retouch_region' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.id=photo.semantic_uuid_v5(r.id,'semantic.adopt_shown_retouch_region:'||target.id::text)
 AND t.node_key='semantic.adopt_shown_retouch_region:'||target.id::text
 AND t.params_artifact_id=photo.semantic_uuid_v5(t.id,'Params')
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') node
 WHERE node->>'task_id'=t.id::text AND node->>'target_id'=target.id::text
 AND node->>'params_artifact_id'=params.id::text AND node->>'params_sha'=params.sha256)
 AND params.state='ready' AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL
 AND params.object_version NOT IN('','null') AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 AND d.sha256=params.sha256 AND d.bytes=params.bytes AND d.bucket=params.bucket AND d.object_version=params.object_version
 AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
 AND q.share_id IS NULL AND q.files=0 AND q.bytes>0 AND q.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_shown_retouch_apply_input_scope(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_retouch_apply_input_scope(uuid,uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE OR REPLACE FUNCTION photo.guard_v2_artifact_link() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE artifact photo.artifacts%ROWTYPE; parent_ok boolean;
BEGIN
 SELECT * INTO artifact FROM photo.artifacts a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.artifact_id;
 IF artifact.id IS NULL OR artifact.sha256<>NEW.sha256 OR artifact.state<>'ready'
  OR artifact.object_version IS NULL OR artifact.verified_at IS NULL
  OR (artifact.expires_at IS NOT NULL AND artifact.expires_at<=clock_timestamp()) THEN
  RAISE EXCEPTION 'V2_ARTIFACT_UNVERIFIED' USING ERRCODE='23514';
 END IF;
 CASE TG_TABLE_NAME
  WHEN 'v2_documents' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_document_streams s
    WHERE s.tenant_id=NEW.tenant_id AND s.project_id=NEW.project_id AND s.id=NEW.stream_id)
    INTO parent_ok;
  WHEN 'v2_document_refs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_documents d
    WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id AND d.id=NEW.document_id)
    INTO parent_ok;
  WHEN 'v2_artifact_links' THEN
   SELECT EXISTS(SELECT 1 FROM photo.v2_workflows w
    WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id AND w.id=NEW.workflow_id)
    INTO parent_ok;
  WHEN 'v2_task_inputs' THEN
   SELECT EXISTS(SELECT 1 FROM photo.tasks t
    JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
    WHERE t.tenant_id=NEW.tenant_id AND t.id=NEW.task_id AND w.project_id=NEW.project_id)
    OR photo.semantic_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_preapply_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_removal_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_background_cleanup_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_retouch_preview_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_shown_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_shown_removal_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_shown_background_cleanup_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    OR photo.semantic_shown_retouch_apply_input_scope(NEW.tenant_id,NEW.project_id,NEW.task_id)
    INTO parent_ok;
  ELSE
   RAISE EXCEPTION 'V2_ARTIFACT_LINK_TABLE_INVALID' USING ERRCODE='23514';
 END CASE;
 IF parent_ok IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'V2_ARTIFACT_LINK_SCOPE_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd

-- Private same-transaction complete input registration, following reserved shell.
-- Exact replay allowed; partial/altered registration always fails, never repaired.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_shown_retouch_apply_inputs(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE row jsonb;entry jsonb;expected jsonb;actual jsonb;task uuid;parent uuid;total integer:=0;
BEGIN
 PERFORM photo.reserve_semantic_shown_retouch_apply_quota(p_tenant,p_project,p_actor,p_child,p_tasks);
 SELECT w.run_id INTO parent FROM photo.v2_review_batches b JOIN photo.v2_workflows w
 ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id
 WHERE b.tenant_id=p_tenant AND b.child_run_id=p_child AND b.project_id=p_project AND b.actor_id=p_actor AND b.action='apply';
 IF NOT FOUND THEN RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUT_BATCH_INVALID' USING ERRCODE='23514';END IF;
 FOR row IN SELECT value FROM jsonb_array_elements(p_tasks) ORDER BY value->'node'->>'id' LOOP
 task:=(row->'node'->>'id')::uuid;
 IF photo.semantic_shown_retouch_apply_input_scope(p_tenant,p_project,task) IS DISTINCT FROM true
 OR photo.semantic_shown_retouch_transport_documents_ready(p_tenant,parent,p_child,(row->>'params_body')::jsonb,
 row->'node',row->'transport',row->'inventory') IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUT_SCOPE_INVALID' USING ERRCODE='23514';END IF;
 SELECT jsonb_agg(jsonb_build_object('ref',ref,'contract',contract,'ordinal',ordinal) ORDER BY ordinal)
 INTO expected FROM (
 SELECT e.value->'ref' AS ref,e.value->>'contract' AS contract,(e.ordinality-1)::integer AS ordinal
 FROM jsonb_array_elements(row->'inventory'->'entries') WITH ORDINALITY e(value,ordinality)
 UNION ALL SELECT row->'transport'->'chunks'->0,'InputClosureChunk',1000000
 UNION ALL SELECT row->'transport'->'manifest','InputClosureManifest',1000001) entries;
 SELECT coalesce(jsonb_agg(jsonb_build_object('ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256),
 'contract',i.contract,'ordinal',i.ordinal) ORDER BY i.ordinal),'[]'::jsonb)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task;
 IF actual<>'[]'::jsonb THEN
 IF actual IS DISTINCT FROM expected OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=task AND i.project_id IS DISTINCT FROM p_project) THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_INPUT_REPLAY_CONFLICT' USING ERRCODE='40001';END IF;
 ELSE
 FOR entry IN SELECT value FROM jsonb_array_elements(expected) LOOP
 INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal)
 VALUES(p_tenant,p_project,task,(entry->'ref'->>'artifact_id')::uuid,entry->'ref'->>'sha256',entry->>'contract',(entry->>'ordinal')::integer);
 END LOOP;
 END IF;
 total:=total+jsonb_array_length(expected);
 END LOOP;
 RETURN total;
END $$;
ALTER FUNCTION photo.register_semantic_shown_retouch_apply_inputs(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_shown_retouch_apply_inputs(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- Persisted draft equality gate for quota/seal. Exact sealed inventory/profile gate; no activation or lease authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_retouch_apply_sealed_inputs_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS boolean
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
 'renderer_version','v2.2','contracts_sha','886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90',
 'v2_input_manifest',row->'transport'->'manifest','v2_entry_count',(row->'transport'->>'entry_count')::integer,
 'v2_entries_sha',row->'transport'->>'entries_sha')
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

-- Seal exact registered inputs, leaving every task blocked and child draft.
-- Caller must activate/finalize in same transaction or roll everything back.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_semantic_shown_retouch_apply_inputs(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS integer
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
 'renderer_version','v2.2','contracts_sha','886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90',
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
