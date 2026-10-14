-- +goose Up
-- Preserve existing purpose predicates; snapshot document here is bounded64KiB.
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_PURPOSE_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v30_interchange_snapshot'')',expression);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v30_interchange_document(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_artifact uuid,p_purpose text,p_body text,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;sha text;size bigint;fence uuid:=gen_random_uuid();used numeric;capacity numeric;
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 -- Ledger before tenant/run/document locks, matching creation admission ordering.
 PERFORM id FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='Interchange.Parse.Pending' AND key=p_key FOR SHARE;
 scope:=photo.v30_interchange_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,p_artifact,p_purpose,p_body);
 IF scope IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;sha:=scope->>'sha';size:=(scope->>'bytes')::bigint;
 IF (p_purpose='task_params' AND size>65536) OR (p_purpose<>'v30_interchange_snapshot' AND size>1048576) THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>p_artifact OR d.sha256<>sha OR d.bytes<>size OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.state='ready'
    AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,rid,owner,p_purpose,p_artifact,fence,sha,size,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v30_interchange_document(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_artifact uuid,p_purpose text,p_body text,p_bucket text,p_token uuid,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor AND route='Interchange.Parse.Pending' AND key=p_key FOR SHARE;
 scope:=photo.v30_interchange_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,p_artifact,p_purpose,p_body);
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM scope->>'sha'
 OR d.bytes IS DISTINCT FROM (scope->>'bytes')::bigint OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=p_version AND a.verified_at IS NOT NULL
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,d.bucket,'tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),p_purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_interchange_document(uuid,uuid,uuid,text,text,uuid,text,text,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
