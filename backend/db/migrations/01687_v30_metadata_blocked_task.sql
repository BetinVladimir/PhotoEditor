-- +goose Up
-- Register metadata task blocked; transport/input closure/activation remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.create_v30_metadata_task(p_tenant uuid,p_expansion uuid,p_token uuid,p_task jsonb,p_reserved bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof_text text;pin text;proof jsonb;e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;
 d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 owner uuid;ref jsonb;used numeric;capacity numeric;estimate numeric;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_reserved IS NULL OR p_reserved NOT BETWEEN 1 AND 9007199254740991
 OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576 THEN RETURN false;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active';
 IF used IS NULL OR capacity IS NULL THEN RETURN false;END IF;
 SELECT proof_jcs,proof_sha INTO proof_text,pin FROM photo.read_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token);
 IF proof_text IS NULL THEN RETURN false;END IF;
 proof:=proof_text::jsonb;owner:=(proof->>'task_id')::uuid;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=e.run_id;
 IF r.id IS NULL OR r.graph_sealed OR r.status<>'running' OR r.cancel_requested THEN RETURN false;END IF;
 IF p_task->>'id' IS DISTINCT FROM owner::text OR p_task->>'node_key' IS DISTINCT FROM 'metadata.write:0'
 OR p_task->>'operation' IS DISTINCT FROM 'metadata.write' OR p_task->>'operation_version' IS DISTINCT FROM '1'
 OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR p_task->>'params_artifact_id' IS DISTINCT FROM proof->'params'->>'artifact_id'
 OR p_task->>'params_sha' IS DISTINCT FROM proof->'params'->>'sha256'
 OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR p_task->'execution_profile'->>'backend' IS DISTINCT FROM 'cpu'
 OR p_task->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR p_task->'execution_profile'->'resource_estimate'->'vram_bytes' IS DISTINCT FROM '0'::jsonb THEN RETURN false;END IF;
 estimate:=(p_task->'execution_profile'->'resource_estimate'->>'output_bytes')::numeric;
 IF estimate IS NULL OR estimate<>p_reserved THEN RETURN false;END IF;
 FOREACH ref IN ARRAY ARRAY[proof->'params',proof->'projection'] LOOP
  SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
  WHERE b.tenant_id=p_tenant AND b.run_id=r.id AND b.owner_id=owner AND b.artifact_id=(ref->>'artifact_id')::uuid
  AND b.sha256=ref->>'sha256' AND b.state='ready'
  AND b.purpose=CASE WHEN ref=proof->'params' THEN 'task_params' ELSE 'v30_metadata_projection' END
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket
  AND a.object_version=b.object_version AND a.object_version NOT IN('','null')
  AND a.media_type='application/json' AND a.output_role=b.purpose
  AND a.object_key='tenants/'||p_tenant||'/runs/'||r.id||'/documents/'||b.artifact_id
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=owner FOR UPDATE;
 IF FOUND THEN RETURN e.expansion_lease_until>clock_timestamp() AND old.run_id=r.id AND old.state='blocked' AND old.attempt=0
 AND old.operation='metadata.write' AND old.operation_version='1' AND old.node_key='metadata.write:0'
 AND old.resource_class='cpu' AND old.params_artifact_id=(proof->'params'->>'artifact_id')::uuid
 AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=p_task->'execution_profile'
 AND EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=r.id
 AND prerequisite_id=e.parent_task_id AND dependent_id=owner AND condition='success');END IF;
 IF used+p_reserved>capacity OR used+p_reserved>9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=r.id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.expires_at<=clock_timestamp() OR q.bytes::numeric+p_reserved>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(owner,p_tenant,r.id,'metadata.write:0','metadata.write','1','cpu','blocked','[]'::jsonb,
 (proof->'params'->>'artifact_id')::uuid,p_task->>'input_digest',p_task->'execution_profile');
 INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition)
 VALUES(p_tenant,r.id,e.parent_task_id,owner,'success');
 UPDATE photo.quota_reservations SET bytes=bytes+p_reserved WHERE id=q.id;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_reserved WHERE tenant_id=p_tenant;
 IF e.expansion_lease_until<=clock_timestamp() THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.create_v30_metadata_task(uuid,uuid,uuid,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_v30_metadata_task(uuid,uuid,uuid,jsonb,bigint) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.create_v30_metadata_task(uuid,uuid,uuid,jsonb,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
