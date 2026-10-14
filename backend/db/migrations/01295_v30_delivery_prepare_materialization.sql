-- +goose Up
-- Materialize one blocked prepare task only after every pinned document is ready.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v30_delivery_prepare(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;params jsonb;scope jsonb;rid uuid;owner uuid;doc jsonb;d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;
 profile jsonb;estimate jsonb;document_bytes bigint:=0;capacity numeric;used numeric;q photo.quota_reservations%ROWTYPE;
BEGIN
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='POST /delivery-packages' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL OR p_task IS NULL THEN RETURN false;END IF;
 SELECT value INTO params FROM jsonb_array_elements(saved->'documents') WHERE value->>'purpose'='task_params';
 scope:=photo.v30_delivery_create_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,(params->'ref'->>'artifact_id')::uuid,'task_params',params->>'body_jcs');
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'params_artifact_id'-'params_sha'-'input_digest'-'execution_profile'<>'{}'::jsonb
 OR p_task->>'id' IS DISTINCT FROM owner::text OR p_task->>'node_key' IS DISTINCT FROM 'v2.delivery.prepare:0'
 OR p_task->>'operation' IS DISTINCT FROM 'v2.delivery.prepare' OR p_task->>'operation_version' IS DISTINCT FROM '1'
 OR p_task->>'resource_class' IS DISTINCT FROM 'cpu' OR p_task->>'params_artifact_id' IS DISTINCT FROM params->'ref'->>'artifact_id'
 OR p_task->>'params_sha' IS DISTINCT FROM params->'ref'->>'sha256' OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 FOR doc IN SELECT value FROM jsonb_array_elements(saved->'documents') LOOP
  SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
  WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.artifact_id=(doc->'ref'->>'artifact_id')::uuid AND b.purpose=doc->>'purpose'
   AND b.owner_id=CASE WHEN b.purpose='task_params' THEN owner ELSE b.artifact_id END
   AND b.sha256=doc->'ref'->>'sha256' AND b.bytes=octet_length(doc->>'body_jcs') AND b.state='ready'
   AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
   AND a.media_type='application/json' AND a.output_role=b.purpose
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
  IF NOT FOUND THEN RETURN false;END IF;
  IF d.purpose<>'v30_delivery_policy' THEN document_bytes:=document_bytes+d.bytes;END IF;
 END LOOP;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';
 IF document_bytes NOT BETWEEN 1 AND 27262976 OR profile-'backend'-'renderer_version'-'resource_estimate'<>'{}'::jsonb
 OR profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR estimate-'ram_bytes'-'vram_bytes'-'scratch_bytes'-'output_bytes'-'pixel_count'<>'{}'::jsonb
 OR estimate->'vram_bytes' IS DISTINCT FROM '0'::jsonb OR estimate->'pixel_count' IS DISTINCT FROM '0'::jsonb
 OR estimate->'output_bytes' IS DISTINCT FROM '8388608'::jsonb
 OR coalesce((estimate->>'ram_bytes')::numeric,0)<536870912+32*document_bytes+2048
 OR coalesce((estimate->>'scratch_bytes')::numeric,0)<33554432+document_bytes+2048 THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=owner FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=rid AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested
 AND old.operation='v2.delivery.prepare' AND old.params_artifact_id=(params->'ref'->>'artifact_id')::uuid
 AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile
 AND EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid
 AND state='reserved' AND bytes=8388608 AND expires_at>clock_timestamp());END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)
 OR EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid) THEN RETURN false;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+8388608>capacity OR used+8388608>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(owner,p_tenant,rid,'v2.delivery.prepare:0','v2.delivery.prepare','1','cpu','blocked','[]'::jsonb,(params->'ref'->>'artifact_id')::uuid,p_task->>'input_digest',profile);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',rid,8388608,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+8388608 WHERE tenant_id=p_tenant;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.materialize_v30_delivery_prepare(uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v30_delivery_prepare(uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.materialize_v30_delivery_prepare(uuid,uuid,uuid,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
