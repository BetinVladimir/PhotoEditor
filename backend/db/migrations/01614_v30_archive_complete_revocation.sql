-- +goose Up
ALTER TABLE photo.v30_archive_writers DROP CONSTRAINT v30_archive_writers_state_check;
ALTER TABLE photo.v30_archive_writers ADD CONSTRAINT v30_archive_writers_state_check CHECK(state IN('open','closed','revoked'));
ALTER TABLE photo.v30_archive_writers DROP CONSTRAINT v30_archive_writers_check;
ALTER TABLE photo.v30_archive_writers ADD CONSTRAINT v30_archive_writers_check CHECK(
 (state='open' AND receipt IS NULL AND closed_at IS NULL)
 OR (state='closed' AND receipt IS NOT NULL AND length(receipt) BETWEEN 1 AND 2048 AND closed_at IS NOT NULL)
 OR (state='revoked' AND kind='complete' AND receipt IS NOT NULL AND receipt='multipart-revoked' AND closed_at IS NOT NULL));
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v30_archive_complete_revocation()
RETURNS TABLE(tenant_id uuid,upload_id uuid,cleanup_token uuid,writer_token uuid,bucket text,object_key text,multipart_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;writer uuid;
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED')
 AND x.cleanup_not_before<=clock_timestamp() AND x.expires_at<=clock_timestamp()
 AND (x.parts_authorized_until IS NULL OR x.parts_authorized_until<=clock_timestamp())
 AND x.completion_lease_until IS NULL AND x.multipart_id IS NOT NULL AND t.status='active'
 AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp())
 AND (SELECT count(*) FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state='open')=1
 AND EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.kind='complete' AND w.state='open')
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 ORDER BY x.cleanup_not_before,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT w.token INTO writer FROM photo.v30_archive_writers w WHERE w.tenant_id=d.tenant_id AND w.upload_id=d.id AND w.kind='complete' AND w.state='open';
 UPDATE photo.v30_archive_uploads x SET cleanup_token=gen_random_uuid(),cleanup_lease_until=clock_timestamp()+interval '2 minutes'
 WHERE x.tenant_id=d.tenant_id AND x.id=d.id RETURNING x.* INTO d;
 RETURN QUERY SELECT d.tenant_id,d.id,d.cleanup_token,writer,d.bucket,d.object_key,d.multipart_id;
END $$;
ALTER FUNCTION photo.claim_v30_archive_complete_revocation() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_archive_complete_revocation() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_v30_archive_complete_revocation() TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_complete_revocation(p_tenant uuid,p_upload uuid,p_cleanup uuid,p_writer uuid,p_bucket text,p_key text,p_multipart text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_cleanup IS NULL OR p_writer IS NULL OR p_multipart IS NULL OR p_multipart='' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.cleanup_token IS DISTINCT FROM p_cleanup OR d.bucket IS DISTINCT FROM p_bucket OR d.object_key IS DISTINCT FROM p_key OR d.multipart_id IS DISTINCT FROM p_multipart
 OR d.object_key<>'tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/archive.zip' OR d.state<>'aborting'
 OR d.error_code IS NULL OR d.error_code NOT IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED')
 OR d.cleanup_lease_until IS NULL OR d.cleanup_lease_until<=clock_timestamp() OR d.cleanup_not_before IS NULL OR d.cleanup_not_before>clock_timestamp()
 OR d.expires_at>clock_timestamp() OR d.parts_authorized_until>clock_timestamp() OR d.completion_lease_until IS NOT NULL THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id=d.artifact_id OR (a.bucket=d.bucket AND a.object_key=d.object_key))) THEN RETURN false;END IF;
 UPDATE photo.v30_archive_writers SET state='revoked',receipt='multipart-revoked',closed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer AND kind='complete' AND state='open';
 IF NOT FOUND THEN RETURN false;END IF;
 -- No quota release or object deletion acknowledgement here. Ordinary cleanup
 -- must independently drain, purge all versions and fence the accounting commit.
 UPDATE photo.v30_archive_uploads SET cleanup_lease_until=NULL WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
ALTER FUNCTION photo.finish_v30_archive_complete_revocation(uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_complete_revocation(uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_complete_revocation(uuid,uuid,uuid,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
