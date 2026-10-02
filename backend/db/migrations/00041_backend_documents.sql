-- +goose Up
CREATE TABLE photo.backend_documents (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL,run_id uuid NOT NULL,
 owner_id uuid NOT NULL,purpose text NOT NULL CHECK(purpose IN ('task_params','review_selection')),
 artifact_id uuid NOT NULL UNIQUE,token uuid NOT NULL,sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 bytes bigint NOT NULL CHECK(bytes BETWEEN 2 AND 1048576),bucket text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready')),
 object_version text,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz,
 UNIQUE(tenant_id,run_id,owner_id,purpose),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 CONSTRAINT backend_documents_version_state CHECK((state='pending' AND object_version IS NULL AND completed_at IS NULL) OR
 (state='ready' AND object_version IS NOT NULL AND object_version<>'' AND object_version<>'null' AND completed_at IS NOT NULL))
);
ALTER TABLE photo.backend_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.backend_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.backend_documents TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.backend_documents TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_backend_document(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE; capacity numeric; used numeric; aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL OR p_purpose IS NULL OR p_purpose NOT IN ('task_params','review_selection')
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN ('queued','running','waiting_review') THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 IF (p_purpose='review_selection' AND NOT EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.id=p_owner AND v.kind='defects' AND v.state='decided'))
 OR (p_purpose='task_params' AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_owner AND e.state='open' AND e.kind LIKE 'review.defects/%')) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=d.object_version) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN; END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_backend_document(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE; target text;
BEGIN
 IF p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN ('queued','running','waiting_review') THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF (p_purpose='review_selection' AND NOT EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.id=p_owner AND v.kind='defects' AND v.state='decided'))
 OR (p_purpose='task_params' AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_owner AND e.state='open' AND e.kind LIKE 'review.defects/%')) THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts a WHERE a.id=d.artifact_id AND a.tenant_id=d.tenant_id AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=p_version);END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,d.tenant_id,d.bucket,target,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),d.purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=d.tenant_id AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
