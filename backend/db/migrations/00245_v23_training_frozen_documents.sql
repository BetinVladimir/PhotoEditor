-- Deterministic frozen recipe documents are pinned by the recovery manifest
-- before the training snapshot is published. They never alias EditingRecipeV3.
-- +goose Up
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check
 CHECK (purpose IN ('task_params','review_selection','v2_input_manifest',
  'v2_session_plan_params','v2_analysis_input_snapshot','v2_analysis_context',
  'v2_analysis_consents','v23_training_manifest','v23_training_snapshot',
  'v23_fit_params','v23_evaluate_params','v23_frozen_base',
  'v23_frozen_approved') OR
  CASE WHEN purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
   THEN substring(purpose from 16)::integer BETWEEN 0 AND 1953 ELSE false END OR
  CASE WHEN purpose ~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'
   THEN true ELSE false END);

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_scope(p_tenant uuid,p_run uuid,
 p_event uuid,p_side text,p_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; recovery jsonb; examples jsonb;
 expected_id uuid; h bytea; profile_id uuid; manifest_sha text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_side IS NULL OR p_side NOT IN ('base','approved') OR p_sha IS NULL OR
  p_sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run;
 IF r.id IS NULL OR r.created_by IS NULL THEN RETURN false; END IF;
 profile_id:=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 manifest_sha:=r.config_snapshot->'v23_training'->>'manifest_sha';
 IF profile_id IS NULL OR NOT photo.v23_training_document_scope(
  p_tenant,p_run,profile_id,'v23_training_manifest',manifest_sha)
  THEN RETURN false; END IF;
 SELECT i.response_body INTO recovery FROM photo.idempotency_records i
  WHERE i.tenant_id=p_tenant AND i.principal_type='user'
   AND i.principal_id=r.created_by AND i.route='V2.Admit'
   AND i.resource_id=p_run AND i.response_status IS NULL;
 IF recovery IS NULL OR recovery->>'manifest_sha' IS DISTINCT FROM
  manifest_sha OR jsonb_typeof(recovery->'manifest_jcs') IS DISTINCT FROM
  'string' THEN RETURN false; END IF;
 examples:=(recovery->>'manifest_jcs')::jsonb->'examples';
 IF jsonb_typeof(examples) IS DISTINCT FROM 'array' OR
  jsonb_array_length(examples) NOT BETWEEN 40 AND 5000 THEN RETURN false; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-frozen/'||p_side||'/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 RETURN (SELECT count(*)=1 FROM jsonb_array_elements(examples) item
  WHERE item->>'event_id'=p_event::text AND
   item->'render_case'->(CASE WHEN p_side='base' THEN 'base_recipe'
    ELSE 'approved_recipe' END)->>'artifact_id'=expected_id::text AND
   item->'render_case'->(CASE WHEN p_side='base' THEN 'base_recipe'
    ELSE 'approved_recipe' END)->>'sha256'=p_sha);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_frozen_scope(uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_scope(uuid,uuid,uuid,text,text)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v23_training_frozen(p_tenant uuid,p_run uuid,
 p_event uuid,p_side text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid; h bytea; fence uuid:=gen_random_uuid(); purpose text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_side IS NULL OR p_side NOT IN ('base','approved') OR p_sha IS NULL OR
  p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR
  p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V23_FROZEN_DOCUMENT' USING ERRCODE='22023';
 END IF;
 purpose:='v23_frozen_'||p_side;
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
  AND b.run_id=p_run AND b.owner_id=p_event AND b.purpose=purpose FOR UPDATE;
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
 VALUES(p_tenant,p_run,p_event,purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v23_training_frozen(uuid,uuid,uuid,text,text,bigint,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v23_training_frozen(uuid,uuid,uuid,text,text,bigint,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v23_training_frozen(uuid,uuid,uuid,text,text,bigint,text)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v23_training_frozen(p_tenant uuid,p_run uuid,
 p_event uuid,p_side text,p_artifact uuid,p_token uuid,p_sha text,
 p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; aid uuid; h bytea; purpose text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_token IS NULL OR p_sha IS NULL OR p_version IS NULL OR
  p_version IN ('','null') OR length(p_version)>1024 OR
  p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_side IS NULL OR p_side NOT IN ('base','approved') THEN RETURN false; END IF;
 purpose:='v23_frozen_'||p_side;
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
  AND b.run_id=p_run AND b.owner_id=p_event AND b.purpose=purpose FOR UPDATE;
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
ALTER FUNCTION photo.complete_v23_training_frozen(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v23_training_frozen(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v23_training_frozen(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_complete(p_tenant uuid,p_run uuid,
 p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; side text; expected jsonb; aid uuid; h bytea;
 event_id uuid; sha text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR jsonb_typeof(p_manifest)<>'object'
  OR jsonb_typeof(p_manifest->'examples')<>'array' OR
  jsonb_array_length(p_manifest->'examples') NOT BETWEEN 40 AND 5000 THEN
  RETURN false;
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  event_id:=NULLIF(item->>'event_id','')::uuid;
  IF event_id IS NULL THEN RETURN false; END IF;
  FOREACH side IN ARRAY ARRAY['base','approved'] LOOP
   expected:=item->'render_case'->(CASE WHEN side='base' THEN
    'base_recipe' ELSE 'approved_recipe' END);
   sha:=expected->>'sha256';
   IF sha IS NULL OR sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
   h:=public.digest(uuid_send(p_run)||convert_to(
    'v23-frozen/'||side||'/'||event_id::text,'UTF8'),'sha1');
   h:=set_byte(h,6,(get_byte(h,6)&15)|80);
   h:=set_byte(h,8,(get_byte(h,8)&63)|128);
   aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
   IF expected->>'artifact_id' IS DISTINCT FROM aid::text OR
    NOT EXISTS(SELECT 1 FROM photo.backend_documents d
     JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND
      a.id=d.artifact_id AND a.sha256=d.sha256 AND
      a.object_version=d.object_version AND a.state='ready'
     WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND
      d.owner_id=event_id AND d.purpose='v23_frozen_'||side AND
      d.artifact_id=aid AND d.sha256=sha AND d.state='ready' AND
      d.object_version IS NOT NULL AND a.output_role='v23_frozen_recipe')
    THEN RETURN false;
   END IF;
  END LOOP;
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 TO photo_scheduler;
