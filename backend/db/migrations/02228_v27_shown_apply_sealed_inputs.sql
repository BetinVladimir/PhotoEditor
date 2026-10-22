-- +goose Up
-- Persisted draft equality gate for quota/seal. Exact sealed inventory/profile gate; no activation or lease authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_apply_sealed_inputs_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS boolean
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
 OR t.operation IS DISTINCT FROM 'semantic.adopt_shown_exposure' OR t.operation_version IS DISTINCT FROM '1'
 OR t.resource_class IS DISTINCT FROM 'cpu' OR t.node_key IS DISTINCT FROM node->>'node_key'
 OR t.params_artifact_id::text IS DISTINCT FROM row->>'params_artifact_id'
 OR t.input_digest IS DISTINCT FROM node->>'input_digest'
 OR t.inputs IS DISTINCT FROM jsonb_build_array(row->'transport'->'manifest')||(row->'transport'->'direct_roots')
 OR t.execution_profile IS DISTINCT FROM (node->'execution_profile')||jsonb_build_object(
 'renderer_version','v2.2','contracts_sha','13f037ae3104206bfe3bdf118bf2e4e174c12682de519cbaaf68afdf8b11dad5',
 'v2_input_manifest',row->'transport'->'manifest','v2_entry_count',(row->'transport'->>'entry_count')::integer,
 'v2_entries_sha',row->'transport'->>'entries_sha')
 OR photo.semantic_shown_exposure_transport_ready(p_child,(row->>'params_body')::jsonb,node,row->'transport',row->'inventory') IS DISTINCT FROM true
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
 OR photo.semantic_shown_exposure_input_objects_ready(p_tenant,row->'inventory'->'entries') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_apply_sealed_inputs_ready(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
