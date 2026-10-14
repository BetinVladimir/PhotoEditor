-- +goose Up
-- Forward-only publication of signed export person-origin receipts. The signer
-- and original producer are qualified by the Go issuer; SQL owns immutable
-- object identity, frozen export scope, quota, reservation fence and replay.
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'MISSING_DOCUMENT_PURPOSE_CONSTRAINT';END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v30_person_origin_receipt'')',expression);
END $$;

CREATE FUNCTION photo.v30_export_person_origin_document_scope(
 p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,
 p_plan text,p_entry uuid,p_mask uuid,p_artifact uuid,p_sha text,p_bytes bigint)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan_id uuid;scope jsonb;plan jsonb;matches integer;
BEGIN
 IF p_plan IS NULL OR octet_length(p_plan) NOT BETWEEN 2 AND 8388608
 OR p_entry IS NULL OR p_mask IS NULL OR p_artifact IS NULL
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 16384
 OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_entry,'v30:person-origin:'||p_mask::text) THEN RETURN NULL;END IF;
 plan_id:=photo.v30_uuid5(photo.v30_uuid5(photo.v30_uuid5(p_tenant,
  'interchange:create:'||p_actor||':'||p_key),'workflow'),'v2.interchange.export:0'),'interchange:plan:1');
 scope:=photo.v30_interchange_export_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
  plan_id,'v30_interchange_export_plan',p_plan);
 IF scope IS NULL THEN RETURN NULL;END IF;
 plan:=p_plan::jsonb;
 IF plan->>'direction' IS DISTINCT FROM 'export' OR plan->>'format' IS DISTINCT FROM 'photoeditor_bundle'
 OR jsonb_typeof(plan->'entries') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 SELECT count(*) INTO matches FROM jsonb_array_elements(plan->'entries') e
 WHERE e->>'entry_id'=p_entry::text AND e->'binding' IS NOT NULL AND e->'recipe' IS NOT NULL;
 IF matches<>1 THEN RETURN NULL;END IF;
 RETURN scope;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_export_person_origin_document_scope(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_person_origin_document_scope(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;

CREATE FUNCTION photo.reserve_v30_export_person_origin_document(
 p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_plan text,
 p_entry uuid,p_mask uuid,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;fence uuid:=gen_random_uuid();used numeric;capacity numeric;
BEGIN
 IF p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
  AND route='Interchange.Export.Pending' AND key=p_key FOR SHARE;
 scope:=photo.v30_export_person_origin_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
  p_plan,p_entry,p_mask,p_artifact,p_sha,p_bytes);
 IF scope IS NULL THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 rid:=(scope->>'run')::uuid;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='draft'
  AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid
  AND b.owner_id=p_artifact AND b.purpose='v30_person_origin_receipt' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id<>p_artifact OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
    AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.state='ready' AND a.bucket=d.bucket
    AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id
    AND a.media_type='application/json' AND a.output_role=d.purpose AND a.object_version=d.object_version
    AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,rid,p_artifact,'v30_person_origin_receipt',p_artifact,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_export_person_origin_document(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_export_person_origin_document(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_export_person_origin_document(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text) TO photo_api;

CREATE FUNCTION photo.complete_v30_export_person_origin_document(
 p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_plan text,
 p_entry uuid,p_mask uuid,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text,p_token uuid,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;d photo.backend_documents%ROWTYPE;rid uuid;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.idempotency_records WHERE tenant_id=p_tenant AND principal_type='user' AND principal_id=p_actor
  AND route='Interchange.Export.Pending' AND key=p_key FOR SHARE;
 scope:=photo.v30_export_person_origin_document_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,
  p_plan,p_entry,p_mask,p_artifact,p_sha,p_bytes);
 IF scope IS NULL THEN RETURN false;END IF;
 rid:=(scope->>'run')::uuid;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=rid AND kind='v2' AND status='draft'
  AND NOT graph_sealed AND NOT cancel_requested AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=rid
  AND b.owner_id=p_artifact AND b.purpose='v30_person_origin_receipt' FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token
 OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.bytes=d.bytes
  AND a.bucket=d.bucket AND a.object_key='tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id
  AND a.media_type='application/json' AND a.output_role=d.purpose AND a.object_version=p_version
  AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,d.bucket,'tenants/'||p_tenant||'/runs/'||rid||'/documents/'||d.artifact_id,
  p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),d.purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_export_person_origin_document(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_export_person_origin_document(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,uuid,text)
 FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_export_person_origin_document(uuid,uuid,uuid,text,text,text,uuid,uuid,uuid,text,bigint,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
