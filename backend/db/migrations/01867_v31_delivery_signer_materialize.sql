-- +goose Up
-- Create only a blocked signer task with quota; transport and activation are
-- separate gates. Scheduler authority comes from the current Delivery lease.
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_v31_delivery_signer_task(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,p_bucket text,
 p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;tenant uuid;actor uuid;rid uuid;tid uuid;pid uuid;
 profile jsonb;estimate jsonb;expected jsonb;params_sha text;
 output bigint;ram bigint;scratch bigint;input_json bigint;
 capacity numeric;used numeric;d photo.backend_documents%ROWTYPE;
 old photo.tasks%ROWTYPE;
BEGIN
 scope:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF scope IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object'
  OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 tenant:=(scope->>'tenant_id')::uuid;actor:=(scope->>'actor_id')::uuid;
 tid:=(scope->>'task_id')::uuid;pid:=photo.v30_uuid5(tid,'params');
 params_sha:=encode(sha256(convert_to(p_params,'UTF8')),'hex');
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';
 ram:=(estimate->>'ram_bytes')::bigint;
 scratch:=(estimate->>'scratch_bytes')::bigint;
 output:=(estimate->>'output_bytes')::bigint;
 input_json:=(profile->>'max_input_json_bytes')::bigint;
 IF ram IS NULL OR ram NOT BETWEEN 1 AND 9007199254740991
  OR input_json IS NULL OR input_json NOT BETWEEN 1048576 AND 134217728
  OR ram<input_json OR input_json<p_lineage_bytes+1048576
  OR scratch IS NULL OR scratch NOT BETWEEN 1073741824 AND 9007199254740991
  OR output IS NULL OR output NOT BETWEEN 553648128 AND 9007199254740991
  OR coalesce(profile->>'contracts_sha','') !~ '^[0-9a-f]{64}$'
  OR coalesce(profile->>'sdk_lock_sha','') !~ '^[0-9a-f]{64}$'
  OR coalesce(profile->>'certificate_chain_sha','') !~ '^[0-9a-f]{64}$'
  OR coalesce(profile->>'trust_bundle_sha','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
 expected:=jsonb_build_object('backend','cpu','renderer_version','v2.2',
  'execution_pool','signer','contracts_sha',profile->>'contracts_sha',
  'sdk_lock_sha',profile->>'sdk_lock_sha',
  'certificate_chain_sha',profile->>'certificate_chain_sha',
  'trust_bundle_sha',profile->>'trust_bundle_sha',
  'max_input_json_bytes',input_json,
  'resource_estimate',jsonb_build_object('ram_bytes',ram,'vram_bytes',0,
   'scratch_bytes',scratch,'output_bytes',output,'pixel_count',0));
 IF profile IS DISTINCT FROM expected
  OR p_task->>'id' IS DISTINCT FROM tid::text
  OR p_task->>'params_artifact_id' IS DISTINCT FROM pid::text
  OR p_task->>'params_sha' IS DISTINCT FROM params_sha
  OR p_task->>'node_key' IS DISTINCT FROM 'provenance.sign:0'
  OR p_task->>'operation' IS DISTINCT FROM 'provenance.sign'
  OR p_task->>'operation_version' IS DISTINCT FROM '1'
  OR p_task->>'resource_class' IS DISTINCT FROM 'cpu'
  OR coalesce(p_task->>'input_digest','') !~ '^[0-9a-f]{64}$'
  OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op
   WHERE op.operation='provenance.sign' AND op.version='1'
    AND op.resource_class='cpu' AND op.backends ? 'cpu') THEN RETURN false;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage
  WHERE tenant_id=tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants
  WHERE id=tenant AND status='active' FOR UPDATE;
 IF used IS NULL OR capacity IS NULL THEN RETURN false;END IF;
 rid:=photo.v31_delivery_signer_params_scope(p_expansion,p_token,p_ordinal,
  p_preset,p_source,p_lineage_bytes,p_params,p_bucket);
 IF rid IS NULL OR photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset) IS DISTINCT FROM scope
  OR p_task->>'input_digest' IS DISTINCT FROM (
   SELECT s.digest FROM photo.snapshots s JOIN photo.runs r
    ON r.tenant_id=s.tenant_id AND r.input_snapshot_id=s.id
    WHERE r.tenant_id=tenant AND r.id=rid) THEN RETURN false;END IF;
 SELECT b.* INTO d FROM photo.backend_documents b JOIN photo.artifacts a
  ON a.tenant_id=b.tenant_id AND a.id=b.artifact_id
  WHERE b.tenant_id=tenant AND b.run_id=rid AND b.owner_id=tid
   AND b.purpose='task_params' AND b.artifact_id=pid AND b.state='ready'
   AND b.sha256=params_sha AND b.bytes=octet_length(p_params)
   AND b.bucket=p_bucket AND a.state='ready' AND a.sha256=b.sha256
   AND a.bytes=b.bytes AND a.bucket=b.bucket
   AND a.object_version=b.object_version AND a.object_version NOT IN('','null')
   AND a.verified_at IS NOT NULL AND a.media_type='application/json'
   AND a.output_role='task_params'
   AND a.object_key='tenants/'||tenant||'/runs/'||rid||'/documents/'||pid
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  FOR SHARE OF b,a;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=tenant AND id=tid FOR UPDATE;
 IF FOUND THEN
  RETURN old.run_id=rid AND old.state='blocked' AND old.attempt=0
   AND old.fencing_token=0 AND NOT old.cancel_requested
   AND old.canonical_result_id IS NULL AND old.node_key='provenance.sign:0'
   AND old.operation='provenance.sign' AND old.operation_version='1'
   AND old.resource_class='cpu' AND old.params_artifact_id=pid
   AND old.input_digest=p_task->>'input_digest'
   AND (old.execution_profile=profile OR
    (old.execution_profile-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha'=profile
     AND photo.v2_input_closure_ready(tid)))
   AND EXISTS(SELECT 1 FROM photo.quota_reservations q
    WHERE q.tenant_id=tenant AND q.holder_type='run' AND q.holder_id=rid
     AND q.principal_type='user' AND q.principal_id=actor
     AND q.state='reserved' AND q.bytes=output
     AND q.expires_at>clock_timestamp());
 END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=tenant AND run_id=rid)
  OR EXISTS(SELECT 1 FROM photo.tasks WHERE id=tid)
  OR EXISTS(SELECT 1 FROM photo.quota_reservations
   WHERE tenant_id=tenant AND holder_type='run' AND holder_id=rid)
  OR used+output>capacity OR used+output>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,
  operation_version,resource_class,state,inputs,params_artifact_id,
  input_digest,execution_profile)
 VALUES(tid,tenant,rid,'provenance.sign:0','provenance.sign','1','cpu',
  'blocked','[]',pid,p_task->>'input_digest',profile);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,
  holder_type,holder_id,bytes,files,expires_at)
 VALUES(tenant,'user',actor,'run',rid,output,0,
  clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+output
  WHERE tenant_id=tenant;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_v31_delivery_signer_task(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_v31_delivery_signer_task(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.materialize_v31_delivery_signer_task(uuid,uuid,integer,uuid,jsonb,bigint,text,text,jsonb)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
