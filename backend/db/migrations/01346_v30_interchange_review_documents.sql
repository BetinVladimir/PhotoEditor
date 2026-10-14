-- +goose Up
-- User-scoped review selection ledger; all object IO occurs between transactions.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v30_interchange_review_publication(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_revision bigint,p_report_sha text) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run uuid;authority jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_PUBLICATION_STALE' USING ERRCODE='40001';END IF;
 SELECT run_id INTO run FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow AND workflow_type='interchange';
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=run FOR UPDATE;
 authority:=photo.v30_interchange_review_context(p_tenant,p_actor,p_session,p_workflow,p_revision,p_report_sha);
 IF authority IS NULL THEN RAISE EXCEPTION 'INTERCHANGE_REVIEW_PUBLICATION_STALE' USING ERRCODE='40001';END IF;
 RETURN run;
END $$;
ALTER FUNCTION photo.lock_v30_interchange_review_publication(uuid,uuid,uuid,uuid,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v30_interchange_review_publication(uuid,uuid,uuid,uuid,bigint,text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v30_interchange_review_document(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_revision bigint,p_action text,p_report_sha text,p_request_sha text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_run uuid; p_owner uuid; p_purpose constant text:='review_selection'; d photo.backend_documents%ROWTYPE; capacity numeric; used numeric;
 aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_action IS NULL OR p_action NOT IN('decide','close') OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL
  OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
  RAISE EXCEPTION 'INTERCHANGE_REVIEW_DOCUMENT_INVALID' USING ERRCODE='22023';
 END IF;
 p_run:=photo.lock_v30_interchange_review_publication(p_tenant,p_workflow,p_actor,p_session,p_revision,p_report_sha);
 p_owner:=photo.v30_uuid5(p_workflow,'interchange:review:'||p_action||':'||p_actor::text||':'||p_request_sha);
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false; RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256
    AND a.object_version=d.object_version AND a.bytes=d.bytes AND a.bucket=d.bucket
    AND a.media_type='application/json' AND a.output_role='review_selection' AND a.verified_at IS NOT NULL
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
ALTER FUNCTION photo.reserve_v30_interchange_review_document(uuid,uuid,uuid,uuid,bigint,text,text,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_interchange_review_document(uuid,uuid,uuid,uuid,bigint,text,text,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_interchange_review_document(uuid,uuid,uuid,uuid,bigint,text,text,text,text,bigint,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v30_interchange_review_document(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid,p_revision bigint,p_action text,p_report_sha text,p_request_sha text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p_run uuid; p_owner uuid; p_purpose constant text:='review_selection'; d photo.backend_documents%ROWTYPE; target text;
BEGIN
 IF p_action IS NULL OR p_action NOT IN('decide','close') OR coalesce(p_request_sha,'') !~ '^[0-9a-f]{64}$' OR p_token IS NULL OR p_version IS NULL OR p_version IN ('','null')
  OR length(p_version)>1024 THEN RETURN false; END IF;
 p_run:=photo.lock_v30_interchange_review_publication(p_tenant,p_workflow,p_actor,p_session,p_revision,p_report_sha);
 p_owner:=photo.v30_uuid5(p_workflow,'interchange:review:'||p_action||':'||p_actor::text||':'||p_request_sha);
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant
  AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact OR
  d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR
  d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
  THEN RETURN false; END IF;
 IF d.state='ready' THEN
  RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a
   WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready'
    AND a.sha256=d.sha256 AND a.object_version=p_version AND a.bytes=d.bytes AND a.bucket=d.bucket
    AND a.media_type='application/json' AND a.output_role='review_selection' AND a.verified_at IS NOT NULL
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
ALTER FUNCTION photo.complete_v30_interchange_review_document(uuid,uuid,uuid,uuid,bigint,text,text,text,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_interchange_review_document(uuid,uuid,uuid,uuid,bigint,text,text,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v30_interchange_review_document(uuid,uuid,uuid,uuid,bigint,text,text,text,uuid,uuid,text,bigint,text,text) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
