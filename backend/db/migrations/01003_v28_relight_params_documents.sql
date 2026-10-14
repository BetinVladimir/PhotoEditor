-- +goose Up
-- Relight publication authority is separate from donor/local/assembly scopes.
-- Proof carries exact original selected/root JSON, never a caller boolean grant.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_params_document_scope(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;subject jsonb;part jsonb;kind text;is_guard boolean;
 seen_subject boolean:=false;seen_guard boolean:=false;origin jsonb;original jsonb;inputs jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_purpose IS DISTINCT FROM 'task_params' OR p_sha IS NULL OR p_bytes NOT BETWEEN 2 AND 65536
 OR jsonb_typeof(p_proof) IS DISTINCT FROM 'object' OR octet_length(p_proof::text)>8388608
 OR (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(p_proof) key)
 IS DISTINCT FROM ARRAY['depth','manual','protected','subject']::text[] THEN RETURN false;END IF;
 SELECT v28_relight_intent INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF request IS NULL OR (request->>'owner_id')::uuid IS DISTINCT FROM p_owner
 OR request->>'params_sha' IS DISTINCT FROM p_sha
 OR octet_length(request->>'params_jcs')::bigint IS DISTINCT FROM p_bytes
 OR photo.save_v28_relight_intent(p_tenant,p_run,request) IS DISTINCT FROM true THEN RETURN false;END IF;
 inputs:=(request->>'inputs_jcs')::jsonb;
 IF jsonb_typeof(p_proof->'depth') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_proof->'subject') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_proof->'protected') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_proof->'manual') IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_proof->'manual') NOT BETWEEN 1 AND 2 THEN RETURN false;END IF;
 origin:=photo.v28_relight_intent_depth_origin(p_tenant,p_run,p_proof->>'depth');
 IF origin IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) i
 WHERE i->>'contract'='Params' AND i->'ref'=jsonb_build_object('artifact_id',origin->>'artifact_id','sha256',origin->>'sha256')) THEN RETURN false;END IF;
 subject:=(p_proof->>'subject')::jsonb;
 IF subject->>'selector'='person' THEN
 origin:=photo.v28_relight_intent_person_origin(p_tenant,p_run,p_proof->>'subject');
 IF origin IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) i
 WHERE i->>'contract'='Params' AND i->'ref'=jsonb_build_object('artifact_id',origin->>'artifact_id','sha256',origin->>'sha256')) THEN RETURN false;END IF;
 seen_subject:=true;
 ELSIF subject->>'selector' IS DISTINCT FROM 'manual' THEN RETURN false;END IF;
 FOR part IN SELECT value FROM jsonb_array_elements(p_proof->'manual') LOOP
 IF jsonb_typeof(part) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(key ORDER BY key) FROM jsonb_object_keys(part) key) IS DISTINCT FROM ARRAY['protected','root']::text[]
 OR jsonb_typeof(part->'protected') IS DISTINCT FROM 'boolean'
 OR jsonb_typeof(part->'root') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 is_guard:=(part->>'protected')::boolean;
 IF is_guard THEN
 IF seen_guard THEN RETURN false;END IF;seen_guard:=true;
 ELSE
 IF seen_subject THEN RETURN false;END IF;seen_subject:=true;
 END IF;
 -- Both companion arrays, root and original Params must remain in the saved
 -- inventory and canonical producer result at reservation AND completion.
 FOREACH kind IN ARRAY ARRAY['root','params','target','guard'] LOOP
 origin:=photo.v28_relight_intent_manual_object(p_tenant,p_run,
 p_proof->>CASE WHEN is_guard THEN 'protected' ELSE 'subject' END,part->>'root',is_guard,kind);
 IF origin IS NULL THEN RETURN false;END IF;
 END LOOP;
 -- Inline vectors refer to original Params; external vectors require the
 -- separately declared ManualMask object and original interactive authority.
 original:=(part->>'root')::jsonb->'vectors';
 IF original IS DISTINCT FROM jsonb_build_object('artifact_id',
 photo.v28_relight_intent_manual_object(p_tenant,p_run,p_proof->>CASE WHEN is_guard THEN 'protected' ELSE 'subject' END,part->>'root',is_guard,'params')->>'artifact_id',
 'sha256',photo.v28_relight_intent_manual_object(p_tenant,p_run,p_proof->>CASE WHEN is_guard THEN 'protected' ELSE 'subject' END,part->>'root',is_guard,'params')->>'sha256')
 AND photo.v28_relight_intent_manual_object(p_tenant,p_run,p_proof->>CASE WHEN is_guard THEN 'protected' ELSE 'subject' END,part->>'root',is_guard,'vectors') IS NULL THEN RETURN false;END IF;
 END LOOP;
 RETURN seen_subject AND seen_guard;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_relight_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_params_document_scope(uuid,uuid,uuid,text,text,bigint,jsonb) TO photo_scheduler;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v28_relight_params_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text,p_proof jsonb)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND 65536
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V28_RELIGHT_DOCUMENT' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V28_RELIGHT_TENANT_INACTIVE' USING ERRCODE='55000'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v28_relight_params_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha,p_bytes,p_proof)
  THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
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
ALTER FUNCTION photo.reserve_v28_relight_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v28_relight_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v28_relight_params_document(uuid,uuid,uuid,text,text,bigint,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v28_relight_params_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text,p_proof jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; target text;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN ('','null')
  OR length(p_version)>1024 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V28_RELIGHT_TENANT_INACTIVE' USING ERRCODE='55000'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v28_relight_params_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha,p_bytes,p_proof)
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
ALTER FUNCTION photo.complete_v28_relight_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v28_relight_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v28_relight_params_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text,jsonb) TO photo_scheduler;


-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
