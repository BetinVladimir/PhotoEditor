-- +goose Up
-- Tenant-local immutable installed policy bytes. Reuses current export access
-- helper, but never the lineage purpose/reservation or a global artifact UUID.
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_PURPOSE_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v31_signing_policy'')',expression);
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_bytes_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_BYTES_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_bytes_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_bytes_check CHECK ((%s) OR (purpose=''v31_signing_policy'' AND bytes BETWEEN 2 AND 1048576))',expression);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v31_signing_policy(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_policy uuid,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean,storage_run uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;d photo.backend_documents%ROWTYPE;fence uuid:=gen_random_uuid();capacity numeric;used numeric;
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_export IS NULL OR p_artifact IS NULL OR p_policy IS NULL OR p_policy='00000000-0000-0000-0000-000000000000'::uuid
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_tenant,'signing:policy:'||p_policy::text||':'||p_sha)
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
 -- Usage first: same order as pending-document cleanup; it cannot claim an
 -- expired reservation concurrently with renewal or completion.
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF used IS NULL OR capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);
 IF rid IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.artifact_id=p_artifact AND b.owner_id=p_artifact AND b.purpose='v31_signing_policy' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>p_artifact OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
  IF d.state='ready' THEN
   PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes
   AND a.state='ready' AND a.bucket=d.bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||d.run_id||'/documents/'||d.artifact_id
   AND a.media_type='application/json' AND a.output_role='v31_signing_policy' AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
   IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true,d.run_id;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence,cleanup_after=clock_timestamp()+interval '3 hours' WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false,d.run_id;RETURN;
 END IF;
 IF used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false,NULL::uuid;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,rid,p_artifact,'v31_signing_policy',p_artifact,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false,rid;
END $$;
ALTER FUNCTION photo.reserve_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v31_signing_policy(p_tenant uuid,p_actor uuid,p_session uuid,p_export uuid,p_expected jsonb,p_policy uuid,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text,p_token uuid,p_storage_run uuid,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rid uuid;d photo.backend_documents%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_storage_run IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_export IS NULL OR p_artifact IS NULL OR p_policy IS NULL OR p_policy='00000000-0000-0000-0000-000000000000'::uuid
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_tenant,'signing:policy:'||p_policy::text||':'||p_sha)
 OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 rid:=photo.v31_signing_lineage_scope(p_tenant,p_actor,p_session,p_export,p_expected,p_bucket);IF rid IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.artifact_id=p_artifact AND b.owner_id=p_artifact AND b.purpose='v31_signing_policy' FOR UPDATE;
 IF NOT FOUND OR d.run_id IS DISTINCT FROM p_storage_run OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha
 OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF d.state='ready' THEN
  IF d.object_version IS DISTINCT FROM p_version THEN RETURN false;END IF;
  PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bucket=d.bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||d.run_id||'/documents/'||d.artifact_id
  AND a.media_type='application/json' AND a.output_role='v31_signing_policy' AND a.object_version=p_version AND a.verified_at IS NOT NULL
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
  RETURN FOUND;
 END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,d.bucket,'tenants/'||p_tenant||'/runs/'||d.run_id||'/documents/'||d.artifact_id,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),'v31_signing_policy');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v31_signing_policy(uuid,uuid,uuid,uuid,jsonb,uuid,uuid,text,bigint,text,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd


-- Preserve the function OID so existing SQL callers also use the new branch.
-- +goose StatementBegin
DO $$ DECLARE body text;updated text; BEGIN
 SELECT prosrc INTO body FROM pg_proc WHERE oid='photo.v2_artifact_has_references(uuid,uuid)'::regprocedure AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql');
 IF body IS NULL OR body !~* '^[[:space:]]*SELECT[[:space:]]' THEN RAISE EXCEPTION 'REFERENCE_FUNCTION_SHAPE_CHANGED';END IF;
 updated:=regexp_replace(body,'^([[:space:]]*SELECT[[:space:]]+)',E'\\1 EXISTS(SELECT FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.artifact_id=p_artifact AND d.purpose=''v31_signing_policy'' AND d.state=''ready'') OR ', 'i');
 IF updated=body THEN RAISE EXCEPTION 'REFERENCE_FUNCTION_PATCH_FAILED';END IF;
 EXECUTE format('CREATE OR REPLACE FUNCTION photo.v2_artifact_has_references(p_tenant uuid,p_artifact uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',updated);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
