-- Initial V22 documents belong to a live admission draft. A document's
-- immutable owner is either the run or the deterministic describe task ID.
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_purpose_check;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_purpose_check
 CHECK (purpose IN ('task_params','review_selection','v2_input_manifest',
  'v2_session_plan_params','v2_analysis_input_snapshot','v2_analysis_context',
  'v2_analysis_consents') OR
  CASE WHEN purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
   THEN substring(purpose from 16)::integer BETWEEN 0 AND 1953 ELSE false END OR
  CASE WHEN purpose ~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'
   THEN true ELSE false END);
ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_bytes_check;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_bytes_check
 CHECK (bytes BETWEEN 2 AND CASE WHEN purpose IN
  ('v2_analysis_input_snapshot','v2_analysis_consents') THEN 8388608 ELSE 1048576 END);

-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_document_scope(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
 ordinal integer; h bytea; expected uuid; workflow uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL OR p_purpose IS NULL
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed
  OR r.cancel_requested OR q.state IS DISTINCT FROM 'reserved'
  OR q.expires_at<=clock_timestamp()
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w
   JOIN photo.tenants t ON t.id=w.tenant_id
   WHERE w.tenant_id=p_tenant AND w.run_id=p_run
    AND w.workflow_type='session' AND w.renderer_version='v2.2'
    AND t.status='active') THEN RETURN false; END IF;
 IF p_purpose IN ('v2_analysis_input_snapshot','v2_analysis_context',
  'v2_analysis_consents') THEN
  RETURN p_owner=p_run AND CASE p_purpose
   WHEN 'v2_analysis_input_snapshot' THEN true
   WHEN 'v2_analysis_context' THEN
    p_sha=r.config_snapshot->'v22_session_plan'->>'context_sha'
   ELSE true END;
 END IF;
 IF p_purpose !~ '^v2_analysis_describe_params_([0-9]|[1-3][0-9])$'
  THEN RETURN false; END IF;
 ordinal:=substring(p_purpose from 29)::integer;
 IF ordinal >= (r.config_snapshot->'v22_session_plan'->>'item_count')::integer
  OR ordinal >= 40 THEN RETURN false; END IF;
 SELECT w.id INTO workflow FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 h:=public.digest(uuid_send(workflow)||convert_to('session.describe:'||ordinal,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 RETURN p_owner=expected;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_document_scope(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_document_scope(uuid,uuid,uuid,text,text) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v22_analysis_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND (CASE WHEN p_purpose IN
   ('v2_analysis_input_snapshot','v2_analysis_consents') THEN 8388608 ELSE 1048576 END)
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INVALID_V22_ANALYSIS_DOCUMENT' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v22_analysis_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha)
  THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false; RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
    AND a.object_version=d.object_version) THEN
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
ALTER FUNCTION photo.reserve_v22_analysis_document(uuid,uuid,uuid,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v22_analysis_document(uuid,uuid,uuid,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v22_analysis_document(uuid,uuid,uuid,text,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v22_analysis_document(p_tenant uuid,p_run uuid,
 p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; target text;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN ('','null')
  OR length(p_version)>1024 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT photo.v22_analysis_document_scope(p_tenant,p_run,p_owner,p_purpose,p_sha)
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
    AND a.sha256=d.sha256 AND a.object_version=p_version); END IF;
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
ALTER FUNCTION photo.complete_v22_analysis_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v22_analysis_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v22_analysis_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) TO photo_scheduler;
