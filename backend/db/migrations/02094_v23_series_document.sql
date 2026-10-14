-- Immutable, recovery-stable series holdout evidence. This migration
-- only authorizes document publication; Fit/Evaluate remain fail-closed.
-- +goose Up
-- +goose StatementBegin
DO $$ DECLARE expression text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO expression FROM pg_constraint
 WHERE conrelid='photo.backend_documents'::regclass AND
  conname='backend_documents_purpose_check';
 IF expression IS NULL THEN RAISE EXCEPTION 'Missing document purpose check'; END IF;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check CHECK ((%s) OR purpose=''v23_series_holdout'')',expression);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_document_scope(
 p_tenant uuid,p_run uuid,p_event uuid,p_project uuid,p_actor uuid,
 p_auth_session uuid,p_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; recovery jsonb; examples jsonb;
 profile_id uuid; manifest_sha text; aid uuid; h bytea;
 source_row record; recovery_items jsonb; recovery_item jsonb;
 body_text text; evidence jsonb;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_project IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run;
 IF r.id IS NULL OR r.kind<>'v2' OR r.created_by IS DISTINCT FROM p_actor OR
  r.cancel_requested THEN RETURN false; END IF;
 profile_id:=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid;
 manifest_sha:=r.config_snapshot->'v23_training'->>'manifest_sha';
 PERFORM id FROM photo.learning_profiles p WHERE p.tenant_id=p_tenant AND
  p.id=profile_id AND p.project_id=p_project;
 IF NOT FOUND OR profile_id IS NULL OR
  NOT photo.v23_training_document_scope(p_tenant,p_run,profile_id,
   'v23_training_manifest',manifest_sha) THEN RETURN false; END IF;
 SELECT i.response_body INTO recovery FROM photo.idempotency_records i
  WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND
   i.principal_id=p_actor AND i.route='V2.Admit' AND
   i.resource_id=p_run AND i.response_status IS NULL;
 IF recovery IS NULL OR recovery->>'manifest_sha' IS DISTINCT FROM
  manifest_sha OR jsonb_typeof(recovery->'manifest_jcs') IS DISTINCT FROM
  'string' OR jsonb_typeof(recovery->'series_jcs') IS DISTINCT FROM
  'string' THEN RETURN false; END IF;
 examples:=(recovery->>'manifest_jcs')::jsonb->'examples';
 IF jsonb_typeof(examples) IS DISTINCT FROM 'array' OR
  jsonb_array_length(examples) NOT BETWEEN 40 AND 5000 THEN RETURN false; END IF;
 IF octet_length(recovery->>'series_jcs') NOT BETWEEN 2 AND 16777216
  THEN RETURN false; END IF;
 recovery_items:=(recovery->>'series_jcs')::jsonb;
 IF jsonb_typeof(recovery_items) IS DISTINCT FROM 'array' OR
  jsonb_array_length(recovery_items) NOT BETWEEN 1 AND 5000 THEN
  RETURN false; END IF;
 BEGIN
  SELECT x.value INTO STRICT recovery_item FROM
   jsonb_array_elements(recovery_items) x
   WHERE x.value->>'event_id'=p_event::text;
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN false;
 END;
 IF jsonb_typeof(recovery_item) IS DISTINCT FROM 'object' OR
  (recovery_item-'event_id'-'body_jcs')<>'{}'::jsonb OR
  jsonb_typeof(recovery_item->'body_jcs') IS DISTINCT FROM 'string' THEN
  RETURN false; END IF;
 body_text:=recovery_item->>'body_jcs';
 IF octet_length(body_text) NOT BETWEEN 2 AND 16384 OR
  encode(public.digest(convert_to(body_text,'UTF8'),'sha256'),'hex')
   IS DISTINCT FROM p_sha THEN RETURN false; END IF;
 evidence:=body_text::jsonb;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-series/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 SELECT * INTO source_row FROM photo.v23_series_event_source(
  p_tenant,p_project,p_actor,p_auth_session,p_event);
 IF NOT FOUND THEN RETURN false; END IF;
 IF jsonb_typeof(evidence) IS DISTINCT FROM 'object' OR
  (SELECT count(*) FROM jsonb_object_keys(evidence))<>11 OR
  evidence->>'schema_version' IS DISTINCT FROM '1' OR
  evidence->>'tenant_id' IS DISTINCT FROM p_tenant::text OR
  evidence->>'event_id' IS DISTINCT FROM p_event::text OR
  evidence->>'asset_version_id' IS DISTINCT FROM
   source_row.asset_version_id::text OR
  evidence->>'apply_run_id' IS DISTINCT FROM
   source_row.apply_run_id::text OR
  evidence->>'item_id' IS DISTINCT FROM source_row.item_id::text OR
  evidence->>'producer_task_id' IS DISTINCT FROM
   source_row.producer_task_id::text OR
  evidence->'source_image' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',source_row.source_image_id,
   'sha256',source_row.source_image_sha) OR
  evidence->'stage_image' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',source_row.stage_image_id,
   'sha256',source_row.stage_image_sha) OR
  evidence->'params' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',source_row.params_id,'sha256',source_row.params_sha) OR
  evidence->'series' IS DISTINCT FROM jsonb_build_object(
   'artifact_id',source_row.series_id,'sha256',source_row.series_sha)
  THEN RETURN false; END IF;
 IF (SELECT count(*) FROM jsonb_array_elements(examples) item
  WHERE item->>'event_id'=p_event::text AND
   item->'render_case'->'series_evidence'->>'artifact_id'=aid::text AND
   item->'render_case'->'series_evidence'->>'sha256'=p_sha AND
   item->'render_case'->'defect_evidence' IS NULL AND
   item->'render_case'->'dust_evidence' IS NULL AND
   item->'render_case'->'detail_evidence' IS NULL AND
   item->'render_case'->'binding'->>'asset_version_id'=
    source_row.asset_version_id::text AND
   item->'render_case'->'working_image'->>'artifact_id'=
    source_row.source_image_id::text AND
   item->'render_case'->'working_image'->>'sha256'=
    source_row.source_image_sha)<>1 THEN RETURN false; END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_document_scope(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_document_scope(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v23_series_document(
 p_tenant uuid,p_run uuid,p_event uuid,p_project uuid,p_actor uuid,
 p_auth_session uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; aid uuid; h bytea;
 capacity numeric; used numeric; fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_project IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V23_SERIES_DOCUMENT' USING ERRCODE='22023';
 END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-series/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_series_document_scope(p_tenant,p_run,p_event,
  p_project,p_actor,p_auth_session,p_sha) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN;
 END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_event AND
  b.purpose='v23_series_holdout' FOR UPDATE;
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
 VALUES(p_tenant,p_run,p_event,'v23_series_holdout',aid,fence,p_sha,
  p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v23_series_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v23_series_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v23_series_document(
 uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v23_series_document(
 p_tenant uuid,p_run uuid,p_event uuid,p_project uuid,p_actor uuid,
 p_auth_session uuid,p_artifact uuid,p_token uuid,p_sha text,
 p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; aid uuid; h bytea;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_event IS NULL OR
  p_project IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR
  p_token IS NULL OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR
  p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 OR
  p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576 OR
  p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RETURN false; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-series/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 aid:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact IS DISTINCT FROM aid THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v23_series_document_scope(p_tenant,p_run,p_event,
  p_project,p_actor,p_auth_session,p_sha) THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_event AND
  b.purpose='v23_series_holdout' FOR UPDATE;
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
  'v23_series_holdout');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,
  storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND
  reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'SERIES_DOCUMENT_QUOTA_ACCOUNTING'
  USING ERRCODE='23514'; END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,
  completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v23_series_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v23_series_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v23_series_document(
 uuid,uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
