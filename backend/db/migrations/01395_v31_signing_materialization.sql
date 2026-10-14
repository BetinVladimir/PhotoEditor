-- +goose Up
-- Blocked task only. Runtime qualification is repeated outside this transaction;
-- input transport registration and sealing/activation remain separate gates.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v31_signing_task(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE saved jsonb;rid uuid;tid uuid;pid uuid;profile jsonb;estimate jsonb;output bigint;ram bigint;scratch bigint;input_json bigint;capacity numeric;used numeric;
 d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;expected jsonb;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT response_body INTO saved FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
 AND route='Provenance.Sign.Pending' AND key=p_key AND request_hash=p_request_sha AND response_status IS NULL AND expires_at>clock_timestamp() FOR UPDATE;
 IF saved IS NULL OR p_task IS DISTINCT FROM saved->'task' THEN RETURN false;END IF;
 tid:=(saved->>'task_id')::uuid;pid:=photo.v30_uuid5(tid,'params');profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';
 ram:=(estimate->>'ram_bytes')::bigint;scratch:=(estimate->>'scratch_bytes')::bigint;output:=(estimate->>'output_bytes')::bigint;input_json:=(profile->>'max_input_json_bytes')::bigint;
 IF ram IS NULL OR ram NOT BETWEEN 1 AND 9007199254740991 OR input_json IS NULL OR input_json NOT BETWEEN 1048576 AND 134217728 OR ram<input_json
 OR input_json<octet_length(saved->>'lineage_jcs')+1048576 OR scratch IS NULL OR scratch NOT BETWEEN 1073741824 AND 9007199254740991
 OR output IS NULL OR output NOT BETWEEN 553648128 AND 9007199254740991
 OR coalesce(profile->>'contracts_sha','') !~ '^[0-9a-f]{64}$' OR coalesce(profile->>'sdk_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(profile->>'certificate_chain_sha','') !~ '^[0-9a-f]{64}$' OR coalesce(profile->>'trust_bundle_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 expected:=jsonb_build_object('backend','cpu','renderer_version','v2.2','execution_pool','signer','contracts_sha',profile->>'contracts_sha',
 'sdk_lock_sha',profile->>'sdk_lock_sha','certificate_chain_sha',profile->>'certificate_chain_sha','trust_bundle_sha',profile->>'trust_bundle_sha','max_input_json_bytes',input_json,
 'resource_estimate',jsonb_build_object('ram_bytes',ram,'vram_bytes',0,'scratch_bytes',scratch,'output_bytes',output,'pixel_count',0));
 IF profile IS DISTINCT FROM expected OR p_task->>'id' IS DISTINCT FROM tid::text OR p_task->>'params_artifact_id' IS DISTINCT FROM pid::text
 OR p_task->>'params_sha' IS DISTINCT FROM encode(sha256(convert_to(saved->>'params_jcs','UTF8')),'hex')
 OR p_task->>'node_key' IS DISTINCT FROM 'provenance.sign:0' OR p_task->>'operation' IS DISTINCT FROM 'provenance.sign'
 OR p_task->>'operation_version' IS DISTINCT FROM '1' OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
 OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$'
 OR NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation='provenance.sign' AND op.version='1' AND op.resource_class='cpu' AND op.backends ? 'cpu') THEN RETURN false;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF used IS NULL OR capacity IS NULL THEN RETURN false;END IF;
 rid:=photo.v31_signing_params_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,tid,pid,p_task->>'params_sha',octet_length(saved->>'params_jcs'),saved->>'bucket');
 IF rid IS NULL THEN RETURN false;END IF;
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
 WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=tid AND b.purpose='task_params' AND b.artifact_id=pid AND b.state='ready'
 AND b.sha256=p_task->>'params_sha' AND b.bytes=octet_length(saved->>'params_jcs') AND b.bucket=saved->>'bucket'
 AND a.state='ready' AND a.sha256=b.sha256 AND a.bytes=b.bytes AND a.bucket=b.bucket AND a.object_version=b.object_version
 AND a.object_version NOT IN('','null') AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||pid AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=tid FOR UPDATE;
 IF FOUND THEN
  RETURN old.run_id=rid AND old.state='blocked' AND old.attempt=0 AND old.fencing_token=0 AND NOT old.cancel_requested
  AND old.canonical_result_id IS NULL AND old.node_key='provenance.sign:0' AND old.operation='provenance.sign' AND old.operation_version='1'
  AND old.resource_class='cpu' AND old.params_artifact_id=pid AND old.input_digest=p_task->>'input_digest'
  AND (old.execution_profile=profile OR (old.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'=profile AND photo.v2_input_closure_ready(tid)))
  AND EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=rid
   AND q.principal_type='user' AND q.principal_id=p_actor AND q.state='reserved' AND q.bytes=output AND q.expires_at>clock_timestamp());
 END IF;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=rid)
 OR EXISTS(SELECT FROM photo.tasks WHERE id=tid)
 OR EXISTS(SELECT FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=rid)
 OR used+output>capacity OR used+output>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(tid,p_tenant,rid,'provenance.sign:0','provenance.sign','1','cpu','blocked','[]',pid,p_task->>'input_digest',profile);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',rid,output,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+output WHERE tenant_id=p_tenant;
 RETURN true;
END $$;
ALTER FUNCTION photo.materialize_v31_signing_task(uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v31_signing_task(uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.materialize_v31_signing_task(uuid,uuid,uuid,text,text,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
