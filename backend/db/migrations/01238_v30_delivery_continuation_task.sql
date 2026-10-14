-- +goose Up
-- Register blocked continuation only; transport and activation remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v30_delivery_continuation_task(p_expansion uuid,p_token uuid,p_slot integer,p_stage integer,
 p_predecessor uuid,p_task jsonb,p_params text,p_reserved bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;request jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 b photo.graph_expansions%ROWTYPE;d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;
 predecessor photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;operation text;owner uuid;required_stage integer;
 capacity numeric;used numeric;estimate numeric;previous text;producer jsonb;input_ref jsonb;
BEGIN
 IF p_task IS NULL OR p_predecessor IS NULL OR p_stage NOT BETWEEN 1 AND 2
 OR p_reserved IS NULL OR p_reserved NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 scope:=photo.v30_delivery_params_scope(p_expansion,p_token,p_slot,p_params);
 IF scope IS NULL THEN RETURN false;END IF;
 request:=photo.v30_delivery_child_request(p_expansion,p_token,p_slot);
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=(request->>'child_run_id')::uuid FOR UPDATE;
 IF NOT FOUND OR r.status<>'running' OR r.graph_sealed OR r.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=r.id AND kind='v30.delivery.export/1' FOR UPDATE;
 IF NOT FOUND OR b.state<>'open' OR b.cursor IS DISTINCT FROM (p_stage-1)::text THEN RETURN false;END IF;
 operation:=p_task->>'operation';owner:=(scope->>'owner')::uuid;
 required_stage:=CASE WHEN operation='derivative.sharpen' OR b.expected_count=1 THEN 1 ELSE 2 END;
 IF operation NOT IN('derivative.sharpen','export.encode') OR p_stage<>required_stage
 OR p_task->>'id' IS DISTINCT FROM owner::text OR p_task->>'node_key' IS DISTINCT FROM operation||':0'
 OR p_task->>'operation_version' IS DISTINCT FROM '1' OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_task->>'params_sha' IS DISTINCT FROM scope->>'sha'
 OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$'
 OR p_task->'execution_profile'->>'backend' IS DISTINCT FROM 'cpu'
 OR p_task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_task->'execution_profile'->'resource_estimate'->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN false;END IF;
 previous:=CASE WHEN operation='derivative.sharpen' THEN 'derivative.resize'
 WHEN ((request->>'config_jcs')::jsonb)->'sharpen' IS NOT NULL THEN 'derivative.sharpen' ELSE 'derivative.resize' END;
 producer:=photo.v30_delivery_stage_result(p_expansion,p_token,p_slot,previous);
 IF producer IS NULL OR producer->>'task_id' IS DISTINCT FROM p_predecessor::text THEN RETURN false;END IF;
 SELECT * INTO predecessor FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=p_predecessor FOR UPDATE;
 IF NOT FOUND OR predecessor.state<>'succeeded' THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND owner_id=owner
 AND purpose='task_params' AND state='ready' AND artifact_id=(p_task->>'params_artifact_id')::uuid AND sha256=scope->>'sha';
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=d.artifact_id
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=e.tenant_id AND id=owner FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=r.id AND old.state='blocked' AND old.attempt=0
 AND old.node_key=p_task->>'node_key' AND old.operation=operation AND old.operation_version='1'
 AND old.params_artifact_id=d.artifact_id AND old.input_digest=p_task->>'input_digest'
 AND old.execution_profile=p_task->'execution_profile'
 AND EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=e.tenant_id AND run_id=r.id AND prerequisite_id=p_predecessor AND dependent_id=owner AND condition='success');END IF;
 estimate:=(p_task->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric;
 IF estimate IS NULL OR estimate<>p_reserved THEN RETURN false;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=e.tenant_id;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=e.tenant_id;
 IF capacity IS NULL OR used IS NULL OR used+p_reserved>capacity OR used+p_reserved>9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=e.tenant_id AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.bytes::numeric+p_reserved>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(owner,e.tenant_id,r.id,p_task->>'node_key',operation,'1','cpu','blocked','[]'::jsonb,d.artifact_id,p_task->>'input_digest',p_task->'execution_profile');
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(e.tenant_id,r.id,p_predecessor,owner,'success');
 UPDATE photo.quota_reservations SET bytes=bytes+p_reserved WHERE id=q.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_reserved WHERE tenant_id=e.tenant_id;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_CONTINUATION_TASK_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v30_delivery_continuation_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_delivery_continuation_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v30_delivery_continuation_task(uuid,uuid,integer,integer,uuid,jsonb,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
