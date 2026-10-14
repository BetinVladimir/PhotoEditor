-- Recovery-stable reviewed sensor-dust holdout document publication.
-- This does not activate Fit/Evaluate for sensor dust.
-- +goose Up
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND
  conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'Missing document purpose check'; END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v23_dust_holdout'')',expression);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.v23_reviewed_dust_document_scope(
 p_tenant uuid,p_run uuid,p_event uuid,p_review uuid,p_actor uuid,
 p_auth_session uuid,p_decision_hash text,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; recovery jsonb; examples jsonb;
 profile_id uuid; project_id uuid; manifest_sha text; aid uuid; h bytea;
 source_row record;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_review IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_decision_hash IS NULL OR p_decision_hash !~ '^[0-9a-f]{64}$' OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run;
 IF r.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM p_actor OR
  r.cancel_requested THEN RETURN false; END IF;
 profile_id:=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 manifest_sha:=r.config_snapshot->'v23_training'->>'manifest_sha';
 SELECT p.project_id INTO project_id FROM photo.learning_profiles p
  WHERE p.tenant_id=p_tenant AND p.id=profile_id;
 IF project_id IS NULL OR profile_id IS NULL OR
  NOT photo.v23_training_document_scope(p_tenant,p_run,profile_id,
   'v23_training_manifest',manifest_sha) THEN RETURN false; END IF;
 SELECT i.response_body INTO recovery FROM photo.idempotency_records i
  WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND
   i.principal_id=p_actor AND i.route='V2.Admit' AND
   i.resource_id=p_run AND i.response_status IS NULL;
 IF recovery IS NULL OR recovery->>'manifest_sha' IS DISTINCT FROM
  manifest_sha OR jsonb_typeof(recovery->'manifest_jcs') IS DISTINCT FROM
  'string' THEN RETURN false; END IF;
 examples:=(recovery->>'manifest_jcs')::jsonb->'examples';
 IF jsonb_typeof(examples) IS DISTINCT FROM 'array' OR
  jsonb_array_length(examples) NOT BETWEEN 40 AND 5000 THEN RETURN false; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-dust/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF (SELECT count(*) FROM jsonb_array_elements(examples) item
  WHERE item->>'event_id'=p_event::text AND
   item->'render_case'->'dust_evidence'->>'artifact_id'=aid::text AND
   item->'render_case'->'dust_evidence'->>'sha256'=p_sha AND
   item->'render_case'->'defect_evidence' IS NULL)<>1 THEN RETURN false; END IF;
 SELECT * INTO source_row FROM photo.v23_reviewed_dust_event_source(
  p_tenant,project_id,p_actor,p_auth_session,p_event);
 IF NOT FOUND OR source_row.review_id IS DISTINCT FROM p_review OR
  source_row.decision_hash IS DISTINCT FROM p_decision_hash THEN
  RETURN false; END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_dust_document_scope(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_dust_document_scope(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v23_reviewed_dust_document(
 p_tenant uuid,p_run uuid,p_event uuid,p_review uuid,p_actor uuid,
 p_auth_session uuid,p_decision_hash text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; aid uuid; h bytea;
 capacity numeric; used numeric; fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_review IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_decision_hash IS NULL OR p_decision_hash !~ '^[0-9a-f]{64}$' OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V23_DUST_DOCUMENT' USING ERRCODE='22023';
 END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-dust/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_reviewed_dust_document_scope(p_tenant,p_run,p_event,
  p_review,p_actor,p_auth_session,p_decision_hash,p_sha) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN;
 END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_event AND
  b.purpose='v23_dust_holdout' FOR UPDATE;
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
 VALUES(p_tenant,p_run,p_event,'v23_dust_holdout',aid,fence,p_sha,
  p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v23_reviewed_dust_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v23_reviewed_dust_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v23_reviewed_dust_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v23_reviewed_dust_document(
 p_tenant uuid,p_run uuid,p_event uuid,p_review uuid,p_actor uuid,
 p_auth_session uuid,p_decision_hash text,p_artifact uuid,p_token uuid,p_sha text,
 p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; aid uuid; h bytea;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_review IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_token IS NULL OR p_decision_hash IS NULL OR
  p_decision_hash !~ '^[0-9a-f]{64}$' OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 OR
  p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RETURN false; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-dust/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact IS DISTINCT FROM aid THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_reviewed_dust_document_scope(p_tenant,p_run,p_event,
  p_review,p_actor,p_auth_session,p_decision_hash,p_sha) THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_event AND
  b.purpose='v23_dust_holdout' FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id<>aid OR d.token<>p_token OR
  d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
  RETURN false; END IF;
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
  'v23_dust_holdout');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND
  reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DUST_DOCUMENT_QUOTA_ACCOUNTING'
  USING ERRCODE='23514'; END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v23_reviewed_dust_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v23_reviewed_dust_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v23_reviewed_dust_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
