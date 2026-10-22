-- +goose Up
-- Persisted draft equality gate for quota/seal. No activation or lease authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_apply_draft_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_child uuid,p_tasks jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;row jsonb;node jsonb;t photo.tasks%ROWTYPE;bindings text;config_raw text;n integer;
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
 OR t.execution_profile IS DISTINCT FROM node->'execution_profile'
 OR photo.semantic_shown_exposure_transport_ready(p_child,(row->>'params_body')::jsonb,node,row->'transport',row->'inventory') IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.semantic_targets target JOIN photo.v2_review_batches batch
 ON batch.tenant_id=target.tenant_id AND batch.id=target.batch_id
 WHERE target.tenant_id=p_tenant AND target.id::text=row->>'target_id' AND target.state='queued'
 AND target.child_run_id=p_child AND batch.child_run_id=p_child AND batch.project_id=p_project
 AND batch.actor_id=p_actor AND batch.action='apply') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_apply_draft_ready(uuid,uuid,uuid,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
