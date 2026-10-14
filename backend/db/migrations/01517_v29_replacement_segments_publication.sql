-- +goose Up
-- Dedicated adopted replacement segment publication; no perspective activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_replacement_segments_document_scope(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE observed record;d jsonb;body text;expected_owner uuid;
BEGIN
 IF p_purpose IS DISTINCT FROM 'task_params' OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_proof) k) IS DISTINCT FROM ARRAY['detection','result_id','root','segments_jcs']::text[] THEN RETURN false;END IF;
 d:=(p_proof->'detection'->>'descriptor_jcs')::jsonb;
 IF d->>'tenant_id' IS DISTINCT FROM p_tenant::text OR d->>'run_id' IS DISTINCT FROM p_run::text THEN RETURN false;END IF;
 SELECT * INTO observed FROM photo.read_v29_replacement_detector_lines(p_tenant,(d->>'task_id')::uuid,p_proof->'detection');
 IF NOT FOUND OR observed.result_id::text IS DISTINCT FROM p_proof->>'result_id'
 OR p_proof->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',observed.artifact_id,'sha256',observed.sha256)
 OR (observed.result_jcs::jsonb)->>'status' IS DISTINCT FROM 'available' THEN RETURN false;END IF;
 body:=p_proof->>'segments_jcs';
 IF body IS NULL OR octet_length(body) NOT BETWEEN 2 AND 1048576
 OR body::jsonb IS DISTINCT FROM (observed.result_jcs::jsonb)->'lines'
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(body,'UTF8')),'hex') OR p_bytes IS DISTINCT FROM octet_length(body) THEN RETURN false;END IF;
 expected_owner:=photo.v30_uuid5((d->>'task_id')::uuid,'V29.ReplacementSegments/v1:'||observed.result_id||':'||observed.artifact_id||':'||observed.sha256||':'||p_sha);
 RETURN p_owner=expected_owner;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_replacement_segments_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_replacement_segments_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v29_replacement_segments_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text,p_proof jsonb)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid; fence uuid:=gen_random_uuid();
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V29_NEGATIVE_DOCUMENT' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V29_NEGATIVE_TENANT_INACTIVE' USING ERRCODE='55000'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF photo.v29_replacement_segments_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha,p_bytes,p_proof) IS DISTINCT FROM true
  THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN; END IF;
 aid:=photo.v30_uuid5(p_owner,'params');
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id IS DISTINCT FROM aid OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
    AND a.object_version=d.object_version AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.media_type='application/json'
    AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
    AND a.output_role='task_params' AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN; END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true; RETURN;
  END IF;
  IF d.state<>'pending' THEN
   RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false; RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity
  OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false; RETURN; END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,
  artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v29_replacement_segments_document(uuid,uuid,uuid,text,text,bigint,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v29_replacement_segments_document(uuid,uuid,uuid,text,text,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v29_replacement_segments_document(uuid,uuid,uuid,text,text,bigint,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v29_replacement_segments_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text,p_proof jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; target text;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN ('','null')
  OR length(p_version)>1024 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V29_NEGATIVE_TENANT_INACTIVE' USING ERRCODE='55000'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF photo.v29_replacement_segments_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha,p_bytes,p_proof) IS DISTINCT FROM true
  THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact OR
  d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR
  d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
  THEN RETURN false; END IF;
 IF d.state='ready' THEN
  RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready'
    AND a.sha256=d.sha256 AND a.object_version=p_version AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.media_type='application/json'
    AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
    AND a.output_role='task_params' AND a.verified_at IS NOT NULL
    AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())); END IF;
 IF d.state<>'pending' THEN RETURN false; END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,
  bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,
  'application/json','ready',clock_timestamp(),p_purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514'; END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v29_replacement_segments_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v29_replacement_segments_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v29_replacement_segments_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
