-- Fenced publication for user-authored immutable V2 JSON documents.
CREATE TABLE photo.v2_draft_uploads(
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 document_id uuid NOT NULL,
 project_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,
 artifact_id uuid NOT NULL,
 sha256 text NOT NULL CHECK(sha256~'^[0-9a-f]{64}$'),
 bytes bigint NOT NULL CHECK(bytes BETWEEN 2 AND 1048576),
 bucket text NOT NULL CHECK(length(bucket) BETWEEN 3 AND 63),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','cleaning','abandoned','ready','retired')),
 token uuid NOT NULL DEFAULT gen_random_uuid(),
 object_version text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 cleanup_after timestamptz NOT NULL DEFAULT clock_timestamp()+interval '3 hours',
 cleanup_lease_until timestamptz,
 PRIMARY KEY(tenant_id,document_id),UNIQUE(tenant_id,artifact_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id),
 CHECK((state IN('ready','retired'))=(object_version IS NOT NULL AND completed_at IS NOT NULL)),
 CHECK((state='cleaning')=(cleanup_lease_until IS NOT NULL))
);
ALTER TABLE photo.v2_draft_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v2_draft_uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v2_draft_uploads TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v2_draft_uploads TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v2_draft_upload(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; project uuid; asset uuid; latest_version uuid;
 capacity numeric; used numeric;
 aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS NULL OR p_version IS NULL OR p_document IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576
  OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
  OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN
  RAISE EXCEPTION 'INVALID_V2_DRAFT_UPLOAD' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT a.project_id,a.id INTO project,asset FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.tenants t ON t.id=v.tenant_id AND t.status='active'
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.review_state='accepted'
  AND a.deleted_at IS NULL AND a.upload_state='verified'
  FOR UPDATE OF a FOR SHARE OF v;
 IF project IS NULL OR NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;
 END IF;
 SELECT id INTO latest_version FROM photo.asset_versions
  WHERE tenant_id=p_tenant AND asset_id=asset ORDER BY version_no DESC LIMIT 1;
 IF latest_version IS DISTINCT FROM p_version THEN
  RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;
 END IF;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF FOUND THEN
  IF d.project_id<>project OR d.asset_version_id<>p_version OR d.sha256<>p_sha
   OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;
  END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
    AND a.sha256=d.sha256 AND a.object_version=d.object_version AND a.state='ready') THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;
   END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.v2_draft_uploads SET token=fence WHERE tenant_id=p_tenant AND document_id=p_document;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;
 END IF;
 INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,token)
 VALUES(p_tenant,p_document,project,p_version,aid,p_sha,p_bytes,p_bucket,fence);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v2_draft_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v2_draft_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v2_draft_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v2_draft_upload(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_object_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_object_version IS NULL OR p_object_version IN('','null') OR length(p_object_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR d.asset_version_id<>p_version OR d.artifact_id<>p_artifact OR d.token<>p_token
  OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_object_version;END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/v2-drafts/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_object_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'document');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DRAFT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v2_draft_uploads SET state='ready',object_version=p_object_version,completed_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v2_draft_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v2_draft_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v2_draft_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_api;

-- Expired pending objects may exist after an uncertain PUT. A claim fences
-- both API completion and reservation replay before storage is touched.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v2_draft_upload_cleanup()
RETURNS TABLE(tenant_id uuid,document_id uuid,artifact_id uuid,bucket text,cleanup_token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.v2_draft_uploads%ROWTYPE; d photo.v2_draft_uploads%ROWTYPE;
 fence uuid:=gen_random_uuid();
BEGIN
 SELECT * INTO hint FROM photo.v2_draft_uploads x
 WHERE (x.state='pending' AND x.cleanup_after<=clock_timestamp())
  OR (x.state='cleaning' AND x.cleanup_lease_until<=clock_timestamp())
 ORDER BY x.cleanup_after,x.artifact_id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_usage.tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO d FROM photo.v2_draft_uploads x
  WHERE x.tenant_id=hint.tenant_id AND x.document_id=hint.document_id FOR UPDATE;
 IF NOT FOUND OR NOT ((d.state='pending' AND d.cleanup_after<=clock_timestamp())
  OR (d.state='cleaning' AND d.cleanup_lease_until<=clock_timestamp()))
  OR EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id) THEN RETURN;END IF;
 UPDATE photo.v2_draft_uploads x SET state='cleaning',token=fence,
  cleanup_lease_until=clock_timestamp()+interval '90 seconds'
  WHERE x.tenant_id=d.tenant_id AND x.document_id=d.document_id;
 RETURN QUERY SELECT d.tenant_id,d.document_id,d.artifact_id,d.bucket,fence;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_v2_draft_upload_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v2_draft_upload_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v2_draft_upload_cleanup() TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_v2_draft_upload_cleanup(p_tenant uuid,p_document uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_document IS NULL OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.v2_draft_uploads x
  WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR d.state<>'cleaning' OR d.token<>p_token
  OR d.cleanup_lease_until<=clock_timestamp() OR
  EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id) THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DRAFT_CLEANUP_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v2_draft_uploads SET state='abandoned',cleanup_lease_until=NULL
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_v2_draft_upload_cleanup(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v2_draft_upload_cleanup(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_v2_draft_upload_cleanup(uuid,uuid,uuid) TO photo_maintenance;

-- Ready uploads remain pinned while clients retry a save. After 24 hours the
-- immutable document tables, if any, own the artifact; existing GC handles an
-- unreferenced artifact after this upload reservation is retired.
-- +goose StatementBegin
CREATE FUNCTION photo.retire_one_v2_draft_upload() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE;
BEGIN
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.state='ready'
  AND x.completed_at<clock_timestamp()-interval '24 hours'
 ORDER BY x.completed_at,x.artifact_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.v2_draft_uploads SET state='retired' WHERE tenant_id=d.tenant_id AND document_id=d.document_id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.retire_one_v2_draft_upload() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.retire_one_v2_draft_upload() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.retire_one_v2_draft_upload() TO photo_maintenance;
