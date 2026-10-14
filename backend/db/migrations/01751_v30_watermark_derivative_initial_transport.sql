-- +goose Up
-- Receipt for blocked initial watermark transport under current source authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_watermark_derivative_initial_transport_ready(p_expansion uuid,p_token uuid,p_slot integer,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;request jsonb;e photo.graph_expansions%ROWTYPE;t photo.tasks%ROWTYPE;
 b photo.graph_expansions%ROWTYPE;d photo.backend_documents%ROWTYPE;child uuid;operation text;required_stage integer;
BEGIN
 IF p_task IS NULL THEN RETURN false;END IF;
 scope:=photo.v30_watermark_derivative_initial_params_scope(p_expansion,p_token,p_slot,p_params);
 IF scope IS NULL THEN RETURN false;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 child:=(request->>'child_run_id')::uuid;
 operation:=p_task->>'operation';
 IF operation NOT IN('derivative.resize','derivative.sharpen') THEN RETURN false;END IF;
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
 IF EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=e.tenant_id AND edge.run_id=child AND edge.dependent_id=t.id) THEN RETURN false;END IF;
 RETURN e.expansion_lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_watermark_derivative_initial_transport_ready(uuid,uuid,integer,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_watermark_derivative_initial_transport_ready(uuid,uuid,integer,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_watermark_derivative_initial_transport_ready(uuid,uuid,integer,jsonb,text) TO photo_scheduler;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
