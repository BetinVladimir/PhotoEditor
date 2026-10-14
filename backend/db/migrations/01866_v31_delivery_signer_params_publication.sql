-- +goose Up
-- Scheduler-only immutable Params publication for a delegated signer draft.
-- Object IO occurs between these functions, never inside their transactions.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v31_delivery_signer_params(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,
 p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;tenant uuid;tid uuid;rid uuid;
 d photo.backend_documents%ROWTYPE;fence uuid:=gen_random_uuid();
 capacity numeric;used numeric;
BEGIN
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 tenant:=(delegation->>'tenant_id')::uuid;
 tid:=(delegation->>'task_id')::uuid;
 IF p_artifact IS NULL OR p_artifact IS DISTINCT FROM photo.v30_uuid5(tid,'params')
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_params IS NULL OR p_sha IS DISTINCT FROM encode(sha256(
   convert_to(p_params,'UTF8')),'hex')
  OR p_bytes IS NULL OR p_bytes IS DISTINCT FROM octet_length(p_params)
  OR p_bytes NOT BETWEEN 2 AND 65536
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage
  WHERE tenant_id=tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants
  WHERE id=tenant AND status='active' FOR UPDATE;
 IF used IS NULL OR capacity IS NULL THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=photo.v31_delivery_signer_params_scope(p_expansion,p_token,
  p_ordinal,p_preset,p_source,p_lineage_bytes,p_params,p_bucket);
 IF rid IS NULL OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=tenant AND run_id=rid)
  OR photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset) IS DISTINCT FROM delegation THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=tenant
  AND b.run_id=rid AND b.owner_id=tid AND b.purpose='task_params' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id IS DISTINCT FROM p_artifact OR d.sha256 IS DISTINCT FROM p_sha
   OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=tenant
    AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes
    AND a.state='ready' AND a.bucket=d.bucket
    AND a.object_key='tenants/'||tenant||'/runs/'||rid||'/documents/'||d.artifact_id
    AND a.media_type='application/json' AND a.output_role='task_params'
    AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
   IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN
   RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence,
   cleanup_after=clock_timestamp()+interval '3 hours' WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 IF used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,
  artifact_id,token,sha256,bytes,bucket)
 VALUES(tenant,rid,tid,'task_params',p_artifact,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v31_delivery_signer_params(uuid,uuid,integer,uuid,jsonb,bigint,text,uuid,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v31_delivery_signer_params(uuid,uuid,integer,uuid,jsonb,bigint,text,uuid,text,bigint,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.reserve_v31_delivery_signer_params(uuid,uuid,integer,uuid,jsonb,bigint,text,uuid,text,bigint,text)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v31_delivery_signer_params(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_source jsonb,p_lineage_bytes bigint,p_params text,
 p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text,
 p_document_token uuid,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE delegation jsonb;tenant uuid;tid uuid;rid uuid;
 d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_document_token IS NULL OR p_version IS NULL OR p_version IN('','null')
  OR length(p_version)>1024 THEN RETURN false;END IF;
 delegation:=photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset);
 IF delegation IS NULL THEN RETURN false;END IF;
 tenant:=(delegation->>'tenant_id')::uuid;
 tid:=(delegation->>'task_id')::uuid;
 IF p_artifact IS NULL OR p_artifact IS DISTINCT FROM photo.v30_uuid5(tid,'params')
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
  OR p_params IS NULL OR p_sha IS DISTINCT FROM encode(sha256(
   convert_to(p_params,'UTF8')),'hex')
  OR p_bytes IS NULL OR p_bytes IS DISTINCT FROM octet_length(p_params)
  OR p_bytes NOT BETWEEN 2 AND 65536
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 rid:=photo.v31_delivery_signer_params_scope(p_expansion,p_token,
  p_ordinal,p_preset,p_source,p_lineage_bytes,p_params,p_bucket);
 IF rid IS NULL OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=tenant AND run_id=rid)
  OR photo.v30_delivery_signer_delegation_scope(
  p_expansion,p_token,p_ordinal,p_preset) IS DISTINCT FROM delegation
  THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=tenant
  AND b.run_id=rid AND b.owner_id=tid AND b.purpose='task_params' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact
  OR d.token IS DISTINCT FROM p_document_token
  OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes
  OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF d.state='ready' THEN
  IF d.object_version IS DISTINCT FROM p_version THEN RETURN false;END IF;
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=tenant
   AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
   AND a.bytes=d.bytes AND a.bucket=d.bucket
   AND a.object_key='tenants/'||tenant||'/runs/'||rid||'/documents/'||d.artifact_id
   AND a.media_type='application/json' AND a.output_role='task_params'
   AND a.object_version=p_version AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  RETURN FOUND;
 END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,
  sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,tenant,d.bucket,
  'tenants/'||tenant||'/runs/'||rid||'/documents/'||d.artifact_id,
  p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),
  'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes WHERE tenant_id=tenant
  AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING'
  USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v31_delivery_signer_params(uuid,uuid,integer,uuid,jsonb,bigint,text,uuid,text,bigint,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v31_delivery_signer_params(uuid,uuid,integer,uuid,jsonb,bigint,text,uuid,text,bigint,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.complete_v31_delivery_signer_params(uuid,uuid,integer,uuid,jsonb,bigint,text,uuid,text,bigint,text,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
