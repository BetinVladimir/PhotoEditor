-- +goose Up
-- Share the existing upload quota/cleanup/purge lifecycle; no new tenant table.
ALTER TABLE photo.v2_draft_uploads ADD COLUMN upload_contract text NOT NULL DEFAULT 'Document';
ALTER TABLE photo.v2_draft_uploads ADD COLUMN mask_width bigint;
ALTER TABLE photo.v2_draft_uploads ADD COLUMN mask_height bigint;
ALTER TABLE photo.v2_draft_uploads DROP CONSTRAINT v2_draft_uploads_bytes_check;
ALTER TABLE photo.v2_draft_uploads ADD CONSTRAINT v2_draft_uploads_payload CHECK (
 (upload_contract='Document' AND mask_width IS NULL AND mask_height IS NULL AND bytes BETWEEN 2 AND 1048576)
 OR (upload_contract='MaskPixels' AND CASE WHEN mask_width BETWEEN 1 AND 200000000 AND mask_height BETWEEN 1 AND 200000000
 THEN mask_height<=200000000/mask_width AND bytes BETWEEN 4*mask_width*mask_height+10 AND 4*mask_width*mask_height+65548 ELSE false END));
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
  IF d.upload_contract<>'Document' OR d.project_id<>project OR d.asset_version_id<>p_version OR d.sha256<>p_sha
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
 IF NOT FOUND OR d.upload_contract<>'Document' OR d.asset_version_id<>p_version OR d.artifact_id<>p_artifact OR d.token<>p_token
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
CREATE FUNCTION photo.reserve_v25_user_mask_pixels(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_sha text,p_bytes bigint,p_bucket text,p_width bigint,p_height bigint)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; project uuid; asset uuid; latest_version uuid;
 capacity numeric; used numeric;
 aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_width IS NULL OR p_height IS NULL OR p_width NOT BETWEEN 1 AND 200000000 OR p_height NOT BETWEEN 1 AND 200000000 THEN RAISE EXCEPTION 'INVALID_MASK_DIMENSIONS' USING ERRCODE='22023';END IF;
 IF p_height>200000000/p_width OR p_bytes IS NULL OR p_bytes NOT BETWEEN 4*p_width*p_height+10 AND 4*p_width*p_height+65548 THEN RAISE EXCEPTION 'INVALID_MASK_BYTES' USING ERRCODE='22023';END IF;
 IF p_tenant IS NULL OR p_version IS NULL OR p_document IS NULL OR p_actor IS NULL OR p_session IS NULL
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 14 AND 800065548
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
  IF d.upload_contract<>'MaskPixels' OR d.mask_width<>p_width OR d.mask_height<>p_height OR d.project_id<>project OR d.asset_version_id<>p_version OR d.sha256<>p_sha
   OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;
  END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
    AND a.sha256=d.sha256 AND a.object_version=d.object_version AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/x-npy' AND a.output_role='mask_pixels' AND a.bytes=d.bytes AND a.bucket=d.bucket) THEN
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
 INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,token,upload_contract,mask_width,mask_height)
 VALUES(p_tenant,p_document,project,p_version,aid,p_sha,p_bytes,p_bucket,fence,'MaskPixels',p_width,p_height);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,text,bigint,text,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,text,bigint,text,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,text,bigint,text,bigint,bigint) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_v25_user_mask_pixels(p_tenant uuid,p_version uuid,p_document uuid,
 p_actor uuid,p_session uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,
 p_bucket text,p_object_version text,p_width bigint,p_height bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v2_draft_uploads%ROWTYPE; target text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_object_version IS NULL OR p_object_version IN('','null') OR length(p_object_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.v2_draft_uploads x WHERE x.tenant_id=p_tenant AND x.document_id=p_document FOR UPDATE;
 IF NOT FOUND OR d.upload_contract<>'MaskPixels' OR d.mask_width IS DISTINCT FROM p_width OR d.mask_height IS DISTINCT FROM p_height OR d.asset_version_id IS DISTINCT FROM p_version OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token
  OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket
  OR NOT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.review_state='accepted' AND a.project_id=d.project_id
   AND a.deleted_at IS NULL AND a.upload_state='verified' AND NOT EXISTS(SELECT 1 FROM photo.asset_versions newer
    WHERE newer.tenant_id=v.tenant_id AND newer.asset_id=v.asset_id AND newer.version_no>v.version_no)) THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_object_version AND EXISTS(SELECT 1 FROM photo.artifacts a
  WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
   AND a.object_version=d.object_version AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.media_type='application/x-npy' AND a.output_role='mask_pixels');END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/v2-drafts/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_object_version,p_sha,p_bytes,'application/x-npy','ready',clock_timestamp(),'mask_pixels');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DRAFT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v2_draft_uploads SET state='ready',object_version=p_object_version,completed_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND document_id=p_document;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint,text,text,bigint,bigint) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
