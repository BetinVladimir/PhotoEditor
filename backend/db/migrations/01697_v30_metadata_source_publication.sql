-- +goose Up
-- API-normalized sources share backend_documents quota/retirement/purge.
-- +goose StatementBegin
DO $$ DECLARE expression text;BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'DOCUMENT_PURPOSE_CONTRACT_MISSING';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v30_metadata_source'')',expression);
END $$;
ALTER TABLE photo.backend_documents ADD COLUMN v30_metadata_source_jcs text;
ALTER TABLE photo.backend_documents ADD COLUMN v30_metadata_source_sha text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT v30_metadata_source_pair CHECK(
 (v30_metadata_source_jcs IS NULL AND v30_metadata_source_sha IS NULL) OR
 (purpose='v30_metadata_source' AND v30_metadata_source_jcs IS NOT NULL AND v30_metadata_source_sha IS NOT NULL
 AND octet_length(v30_metadata_source_jcs) BETWEEN 2 AND 1048576
 AND v30_metadata_source_sha=encode(sha256(convert_to(v30_metadata_source_jcs,'UTF8')),'hex')));
CREATE FUNCTION photo.guard_v30_metadata_source_publication() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.v30_metadata_source_jcs IS NOT NULL AND
 (NEW.v30_metadata_source_jcs IS DISTINCT FROM OLD.v30_metadata_source_jcs OR NEW.v30_metadata_source_sha IS DISTINCT FROM OLD.v30_metadata_source_sha) THEN
 RAISE EXCEPTION 'IMMUTABLE_METADATA_SOURCE_PUBLICATION' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
ALTER FUNCTION photo.guard_v30_metadata_source_publication() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v30_metadata_source_publication() FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE TRIGGER immutable_v30_metadata_source_publication BEFORE UPDATE OF v30_metadata_source_jcs,v30_metadata_source_sha ON photo.backend_documents
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v30_metadata_source_publication();
CREATE FUNCTION photo.v30_metadata_source_scope(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,
 p_body text,p_pin text,p_policy text,p_approved text[]) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE envelope jsonb;s jsonb;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 envelope:=p_body::jsonb;s:=envelope->'scope';
 IF photo.v30_metadata_source_authorized(p_tenant,(s->>'project_id')::uuid,p_actor,p_session,s,p_body,p_pin,
 envelope->'metadata_source',envelope->>'metadata_jcs',p_policy,p_approved) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor THEN RETURN NULL;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 IF NOT FOUND OR w.workflow_type<>'delivery' OR w.renderer_version<>'v2.2' OR w.project_id::text IS DISTINCT FROM s->>'project_id' THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('run',p_run,'owner',envelope->'metadata_source'->>'artifact_id',
 'artifact',envelope->'metadata_source'->>'artifact_id','sha',envelope->'metadata_source'->>'sha256','bytes',octet_length(envelope->>'metadata_jcs'));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_metadata_source_scope(uuid,uuid,uuid,uuid,text,text,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_source_scope(uuid,uuid,uuid,uuid,text,text,text,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE FUNCTION photo.reserve_v30_metadata_source(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_body text,p_pin text,p_policy text,p_approved text[],p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_purpose constant text:='v30_metadata_source';scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;aid uuid;sha text;size bigint;used numeric;capacity numeric;fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 -- Usage before run/expansion/document locks matches cancellation/retirement.
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active';
 IF used IS NULL OR capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 scope:=photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,p_body,p_pin,p_policy,p_approved);
 IF scope IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;aid:=(scope->>'artifact')::uuid;sha:=scope->>'sha';size:=(scope->>'bytes')::bigint;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>aid OR d.sha256<>sha OR d.bytes<>size OR d.bucket<>p_bucket OR d.v30_metadata_source_jcs IS DISTINCT FROM p_body OR d.v30_metadata_source_sha IS DISTINCT FROM p_pin THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=aid AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.sha256=sha AND a.bytes=size AND a.bucket=p_bucket AND a.object_version=d.object_version AND a.media_type='application/json' AND a.output_role=p_purpose
    AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||aid AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',aid,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  IF photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,p_body,p_pin,p_policy,p_approved) IS NULL THEN RAISE EXCEPTION 'DELIVERY_METADATA_SOURCE_AUTHORITY_CHANGED' USING ERRCODE='40001';END IF;
  RETURN QUERY SELECT 'resumed',aid,fence,false;RETURN;
 END IF;
 IF used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,v30_metadata_source_jcs,v30_metadata_source_sha)
 VALUES(p_tenant,rid,owner,p_purpose,aid,fence,sha,size,p_bucket,p_body,p_pin);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
  IF photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,p_body,p_pin,p_policy,p_approved) IS NULL THEN RAISE EXCEPTION 'DELIVERY_METADATA_SOURCE_AUTHORITY_CHANGED' USING ERRCODE='40001';END IF;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text) TO photo_api;

CREATE FUNCTION photo.complete_v30_metadata_source(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_body text,p_pin text,p_policy text,p_approved text[],p_bucket text,p_reservation uuid,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_purpose constant text:='v30_metadata_source';scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;owner uuid;aid uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_reservation IS NULL
 OR p_version IS NULL OR p_version IN('','null') OR octet_length(p_version)>1024 OR position(chr(10) IN p_version)>0 OR position(chr(13) IN p_version)>0 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 scope:=photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,p_body,p_pin,p_policy,p_approved);
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;owner:=(scope->>'owner')::uuid;aid:=(scope->>'artifact')::uuid;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid AND b.owner_id=owner AND b.purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id<>aid OR d.token<>p_reservation OR d.sha256 IS DISTINCT FROM scope->>'sha'
 OR d.bytes IS DISTINCT FROM (scope->>'bytes')::bigint OR d.bucket IS DISTINCT FROM p_bucket OR d.v30_metadata_source_jcs IS DISTINCT FROM p_body OR d.v30_metadata_source_sha IS DISTINCT FROM p_pin THEN RETURN false;END IF;
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
 IF photo.v30_metadata_source_scope(p_tenant,p_run,p_actor,p_session,p_body,p_pin,p_policy,p_approved) IS NULL THEN
 RAISE EXCEPTION 'DELIVERY_METADATA_SOURCE_AUTHORITY_CHANGED' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text,uuid,text) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_metadata_source(uuid,uuid,uuid,uuid,text,text,text,text[],text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
