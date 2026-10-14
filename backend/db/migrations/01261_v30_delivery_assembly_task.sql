-- +goose Up
-- Blocked assemble shell only; complete inventory/source gate required to activate.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v30_delivery_assembly_task(p_expansion uuid,p_token uuid,p_task jsonb,p_params text,p_reserved bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 predecessor photo.tasks%ROWTYPE;owner uuid;capacity numeric;used numeric;estimate numeric;
BEGIN
 IF jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR p_reserved IS DISTINCT FROM 8388608::bigint THEN RETURN false;END IF;
 scope:=photo.v30_delivery_assembly_params_scope(p_expansion,p_token,p_params);
 IF scope IS NULL THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 IF r.id IS NULL OR r.status<>'running' OR r.graph_sealed OR r.cancel_requested
 OR e.expansion_token IS DISTINCT FROM p_token OR e.expansion_lease_until<=clock_timestamp()
 OR e.state<>'open' OR e.cursor::integer<>e.expected_count THEN RETURN false;END IF;
 owner:=(scope->>'owner')::uuid;
 IF p_task->>'id' IS DISTINCT FROM owner::text OR p_task->>'node_key' IS DISTINCT FROM 'v2.delivery.assemble:0'
 OR p_task->>'operation' IS DISTINCT FROM 'v2.delivery.assemble'
 OR p_task->>'operation_version' IS DISTINCT FROM '1' OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_task->>'params_sha' IS DISTINCT FROM scope->>'sha'
 OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR p_task->'execution_profile'->>'backend' IS DISTINCT FROM 'cpu'
 OR p_task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_task->'execution_profile'->'resource_estimate'->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN false;END IF;
 estimate:=(p_task->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric;
 IF estimate IS NULL OR estimate<>p_reserved THEN RETURN false;END IF;
 SELECT * INTO predecessor FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=r.id AND id=e.parent_task_id FOR UPDATE;
 IF predecessor.id IS NULL OR predecessor.state<>'succeeded' OR predecessor.cancel_requested
 OR predecessor.operation<>'v2.delivery.prepare' OR predecessor.canonical_result_id IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=e.tenant_id AND run_id=r.id AND owner_id=owner
 AND purpose='task_params' AND state='ready' AND artifact_id=(p_task->>'params_artifact_id')::uuid AND sha256=scope->>'sha';
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=e.tenant_id AND a.id=d.artifact_id
 AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_key='tenants/'||e.tenant_id||'/runs/'||r.id||'/documents/'||d.artifact_id
 AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=e.tenant_id AND id=owner FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=r.id AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested
 AND old.node_key='v2.delivery.assemble:0' AND old.operation='v2.delivery.assemble' AND old.operation_version='1'
 AND old.resource_class='cpu' AND old.params_artifact_id=d.artifact_id AND old.input_digest=p_task->>'input_digest'
 AND old.execution_profile=p_task->'execution_profile'
 AND EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=e.tenant_id AND run_id=r.id AND prerequisite_id=predecessor.id AND dependent_id=owner AND condition='success');END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=e.tenant_id;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=e.tenant_id FOR UPDATE;
 IF capacity IS NULL OR used IS NULL OR used+p_reserved>capacity OR used+p_reserved>9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=e.tenant_id AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.bytes::numeric+p_reserved>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(owner,e.tenant_id,r.id,'v2.delivery.assemble:0','v2.delivery.assemble','1','cpu','blocked','[]'::jsonb,d.artifact_id,p_task->>'input_digest',p_task->'execution_profile');
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(e.tenant_id,r.id,predecessor.id,owner,'success');
 UPDATE photo.quota_reservations SET bytes=bytes+p_reserved WHERE id=q.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_reserved WHERE tenant_id=e.tenant_id;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=e.id AND expansion_token=p_token AND expansion_lease_until>clock_timestamp()) THEN
  RAISE EXCEPTION 'DELIVERY_ASSEMBLY_TASK_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v30_delivery_assembly_task(uuid,uuid,jsonb,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_delivery_assembly_task(uuid,uuid,jsonb,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_v30_delivery_assembly_task(uuid,uuid,jsonb,text,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
