-- +goose Up
-- Disambiguate frozen document purpose variables without changing protocol.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_v23_training_frozen(p_tenant uuid,p_run uuid,
 p_event uuid,p_side text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid; h bytea; fence uuid:=gen_random_uuid(); frozen_purpose text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_side IS NULL OR p_side NOT IN ('base','approved') OR p_sha IS NULL OR
  p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR
  p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V23_FROZEN_DOCUMENT' USING ERRCODE='22023';
 END IF;
 frozen_purpose:='v23_frozen_'||p_side;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-frozen/'||p_side||'/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_training_frozen_scope(p_tenant,p_run,p_event,p_side,p_sha)
  THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN;
 END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_event AND b.purpose=frozen_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>aid OR d.sha256<>p_sha OR d.bytes<>p_bytes OR
   d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN;
  END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=aid AND a.sha256=p_sha AND a.state='ready' AND
    a.object_version=d.object_version) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN;
   END IF;
   RETURN QUERY SELECT 'replayed',aid,d.token,true; RETURN;
  END IF;
  IF d.state<>'pending' THEN
   RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN;
  END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',aid,fence,false; RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants
  WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage
  WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR
  used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false; RETURN;
 END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,
  artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,p_run,p_event,frozen_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.complete_v23_training_frozen(p_tenant uuid,p_run uuid,
 p_event uuid,p_side text,p_artifact uuid,p_token uuid,p_sha text,
 p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; aid uuid; h bytea; frozen_purpose text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_token IS NULL OR p_sha IS NULL OR p_version IS NULL OR
  p_version IN ('','null') OR length(p_version)>1024 OR
  p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_side IS NULL OR p_side NOT IN ('base','approved') THEN RETURN false; END IF;
 frozen_purpose:='v23_frozen_'||p_side;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-frozen/'||p_side||'/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact IS DISTINCT FROM aid THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_training_frozen_scope(p_tenant,p_run,p_event,p_side,p_sha)
  THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_event AND b.purpose=frozen_purpose FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id<>aid OR d.token<>p_token OR
  d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
  RETURN false;
 END IF;
 IF d.state='ready' THEN
  RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=aid AND a.sha256=p_sha AND
    a.state='ready' AND a.object_version=p_version);
 END IF;
 IF d.state<>'pending' THEN RETURN false; END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,
  sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(aid,p_tenant,p_bucket,
  'tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||aid,
  p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),
  'v23_frozen_recipe');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND
  reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'FROZEN_QUOTA_ACCOUNTING'
  USING ERRCODE='23514'; END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
