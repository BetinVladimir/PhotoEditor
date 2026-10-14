-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_v30_archive_cleanup() RETURNS SETOF photo.v30_archive_uploads
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED') AND x.cleanup_not_before<=clock_timestamp()
 AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp()) AND t.status='active'
 AND x.expires_at<=clock_timestamp() AND (x.parts_authorized_until IS NULL OR x.parts_authorized_until<=clock_timestamp())
 AND x.completion_lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state='open')
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 ORDER BY x.cleanup_not_before,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.v30_archive_uploads SET cleanup_token=gen_random_uuid(),cleanup_lease_until=clock_timestamp()+interval '2 minutes'
 WHERE tenant_id=d.tenant_id AND id=d.id RETURNING * INTO d;
 RETURN NEXT d;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
