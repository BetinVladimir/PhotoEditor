-- +goose Up
-- Share immutable publication and the existing janitor while making the two
-- ticket scopes mutually exclusive. Older draft functions use NULL-safe checks.
ALTER TABLE photo.v2_draft_uploads ALTER COLUMN asset_version_id DROP NOT NULL;
ALTER TABLE photo.v2_draft_uploads ADD COLUMN shoot_session_id uuid;
ALTER TABLE photo.v2_draft_uploads ADD CONSTRAINT v2_upload_scope CHECK(
 (asset_version_id IS NOT NULL) <> (shoot_session_id IS NOT NULL));
ALTER TABLE photo.v2_draft_uploads ADD CONSTRAINT v2_upload_shoot_session
 FOREIGN KEY(tenant_id,project_id,shoot_session_id)
 REFERENCES photo.shoot_sessions(tenant_id,project_id,id) DEFERRABLE INITIALLY DEFERRED;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_v2_draft_upload(p_tenant uuid,p_version uuid,p_document uuid,
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
  IF d.project_id<>project OR d.asset_version_id IS DISTINCT FROM p_version OR d.sha256<>p_sha
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
CREATE OR REPLACE FUNCTION photo.complete_v2_draft_upload(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_object_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_object_version IS NULL OR p_object_version IN('','null') OR length(p_object_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR p_version IS NULL OR d.shoot_session_id IS NOT NULL OR d.asset_version_id IS DISTINCT FROM p_version OR d.artifact_id<>p_artifact OR d.token<>p_token
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

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v24_shot_upload(p_tenant uuid,p_version uuid,p_document uuid,
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
  RAISE EXCEPTION 'INVALID_V24_SHOT_UPLOAD' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT session_row.project_id INTO project FROM photo.shoot_sessions session_row
 JOIN photo.tenants t ON t.id=session_row.tenant_id AND t.status='active'
 WHERE session_row.tenant_id=p_tenant AND session_row.id=p_version AND session_row.state='open'
 FOR SHARE OF session_row;
 IF project IS NULL OR NOT photo.v2_editor_authorized(p_tenant,project,p_actor,p_session) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;
 END IF;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF FOUND THEN
  IF d.project_id<>project OR d.shoot_session_id IS DISTINCT FROM p_version OR d.sha256<>p_sha
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
 INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,shoot_session_id,artifact_id,sha256,bytes,bucket,token)
 VALUES(p_tenant,p_document,project,p_version,aid,p_sha,p_bytes,p_bucket,fence);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v24_shot_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v24_shot_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v24_shot_upload(uuid,uuid,uuid,uuid,uuid,text,bigint,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v24_shot_upload(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_object_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_object_version IS NULL OR p_object_version IN('','null') OR length(p_object_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM id FROM photo.shoot_sessions WHERE tenant_id=p_tenant AND id=p_version AND state='open' FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR p_version IS NULL OR d.asset_version_id IS NOT NULL OR d.shoot_session_id IS DISTINCT FROM p_version OR d.artifact_id<>p_artifact OR d.token<>p_token
  OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.shoot_sessions session_row WHERE session_row.tenant_id=p_tenant AND session_row.id=p_version AND session_row.project_id=d.project_id AND session_row.state='open') THEN RETURN false; END IF;
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
ALTER FUNCTION photo.complete_v24_shot_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v24_shot_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v24_shot_upload(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
