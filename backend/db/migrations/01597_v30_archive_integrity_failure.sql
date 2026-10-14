-- +goose Up
ALTER TABLE photo.v30_archive_uploads ADD COLUMN error_code text CHECK(error_code IS NULL OR error_code IN('ARCHIVE_INTEGRITY','UPLOAD_ABORTED','UPLOAD_EXPIRED'));
-- +goose StatementBegin
CREATE FUNCTION photo.fail_v30_archive_completion(p_tenant uuid,p_upload uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 -- Retain quota until an independently fenced cleanup confirms physical purge.
 UPDATE photo.v30_archive_uploads SET state='aborting',error_code='ARCHIVE_INTEGRITY',completion_lease_until=NULL
 WHERE tenant_id=p_tenant AND id=p_upload AND state='completing' AND completion_token=p_token AND completion_lease_until>clock_timestamp();
 IF FOUND THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload AND state='aborting' AND error_code='ARCHIVE_INTEGRITY' AND completion_token=p_token);
END $$;
ALTER FUNCTION photo.fail_v30_archive_completion(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v30_archive_completion(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.fail_v30_archive_completion(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
