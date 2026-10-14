-- +goose Up
-- Uses existing backend_documents quota, retention and tenant purge lifecycle.
-- +goose StatementBegin
DO $$ DECLARE expression text;BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'DOCUMENT_PURPOSE_CONTRACT_MISSING';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v30_metadata_projection'')',expression);
END $$;
CREATE FUNCTION photo.v30_metadata_document_scope(p_tenant uuid,p_expansion uuid,p_token uuid,p_purpose text,p_body text) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE proof jsonb;body text;pin text;ref jsonb;rid uuid;
BEGIN
 IF p_purpose IS NULL OR p_purpose NOT IN('task_params','v30_metadata_projection') OR p_body IS NULL THEN RETURN NULL;END IF;
 SELECT proof_jcs,proof_sha INTO body,pin FROM photo.read_v30_delivery_metadata_proof(p_tenant,p_expansion,p_token);
 IF body IS NULL THEN RETURN NULL;END IF;
 proof:=body::jsonb;
 IF p_purpose='task_params' THEN ref:=proof->'params';body:=proof->>'params_jcs';ELSE ref:=proof->'projection';body:=proof->>'projection_jcs';END IF;
 IF body IS DISTINCT FROM p_body OR octet_length(body) NOT BETWEEN 2 AND 1048576
 OR (p_purpose='task_params' AND octet_length(body)>65536) THEN RETURN NULL;END IF;
 SELECT run_id INTO rid FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion;
 RETURN jsonb_build_object('run',rid,'owner',proof->>'task_id','artifact',ref->>'artifact_id','sha',ref->>'sha256','bytes',octet_length(body));
END $$;
ALTER FUNCTION photo.v30_metadata_document_scope(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_document_scope(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.reserve_v30_metadata_document(p_tenant uuid,p_expansion uuid,p_token uuid,p_purpose text,p_body text,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;aid uuid;sha text;size bigint;used numeric;capacity numeric;fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 -- Usage before run/expansion/document locks matches cancellation/retirement.
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active';
 IF used IS NULL OR capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 scope:=photo.v30_metadata_document_scope(p_tenant,p_expansion,p_token,p_purpose,p_body);
 IF scope IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;aid:=(scope->>'artifact')::uuid;sha:=scope->>'sha';size:=(scope->>'bytes')::bigint;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>aid OR d.sha256<>sha OR d.bytes<>size OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=aid AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.sha256=sha AND a.bytes=size AND a.bucket=p_bucket AND a.object_version=d.object_version AND a.media_type='application/json' AND a.output_role=p_purpose
    AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||aid AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',aid,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND state='open' AND expansion_lease_until>clock_timestamp()) THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
  RETURN QUERY SELECT 'resumed',aid,fence,false;RETURN;
 END IF;
 IF used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,rid,owner,p_purpose,aid,fence,sha,size,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
  IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND state='open' AND expansion_lease_until>clock_timestamp()) THEN RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_metadata_document(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_metadata_document(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_metadata_document(uuid,uuid,uuid,text,text,text) TO photo_scheduler;

CREATE FUNCTION photo.complete_v30_metadata_document(p_tenant uuid,p_expansion uuid,p_token uuid,p_purpose text,p_body text,p_bucket text,p_reservation uuid,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;aid uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_reservation IS NULL
 OR p_version IS NULL OR p_version IN('','null') OR octet_length(p_version)>1024 OR position(chr(10) IN p_version)>0 OR position(chr(13) IN p_version)>0 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 scope:=photo.v30_metadata_document_scope(p_tenant,p_expansion,p_token,p_purpose,p_body);
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;aid:=(scope->>'artifact')::uuid;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id<>aid OR d.token<>p_reservation OR d.sha256 IS DISTINCT FROM scope->>'sha'
 OR d.bytes IS DISTINCT FROM (scope->>'bytes')::bigint OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=aid
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=p_version
 AND a.media_type='application/json' AND a.output_role=p_purpose AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||aid
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(aid,p_tenant,d.bucket,'tenants/'||p_tenant||'/runs/'||rid||'/documents/'||aid,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),p_purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions WHERE id=p_expansion AND expansion_token=p_token AND state='open' AND expansion_lease_until>clock_timestamp()) THEN
 RAISE EXCEPTION 'DELIVERY_METADATA_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_metadata_document(uuid,uuid,uuid,text,text,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_metadata_document(uuid,uuid,uuid,text,text,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_metadata_document(uuid,uuid,uuid,text,text,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
