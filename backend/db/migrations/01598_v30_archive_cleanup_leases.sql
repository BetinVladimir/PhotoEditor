-- +goose Up
ALTER TABLE photo.v30_archive_uploads ADD COLUMN cleanup_token uuid;
ALTER TABLE photo.v30_archive_uploads ADD COLUMN cleanup_lease_until timestamptz;
ALTER TABLE photo.v30_archive_uploads ADD COLUMN cleanup_not_before timestamptz;
CREATE INDEX ON photo.v30_archive_uploads(cleanup_not_before,id) WHERE state='aborting';
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.fail_v30_archive_completion(p_tenant uuid,p_upload uuid,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_uploads SET state='aborting',error_code='ARCHIVE_INTEGRITY',completion_lease_until=NULL,
 cleanup_not_before=greatest(expires_at,clock_timestamp())+interval '2 hours 5 minutes'
 WHERE tenant_id=p_tenant AND id=p_upload AND state='completing' AND completion_token=p_token AND completion_lease_until>clock_timestamp();
 IF FOUND THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload AND state='aborting' AND error_code='ARCHIVE_INTEGRITY' AND completion_token=p_token);
END $$;
-- Previously failed rows are conservatively delayed from migration time.
UPDATE photo.v30_archive_uploads SET cleanup_not_before=greatest(expires_at,clock_timestamp())+interval '2 hours 5 minutes' WHERE state='aborting';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v30_archive_cleanup() RETURNS SETOF photo.v30_archive_uploads
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state='aborting' AND x.error_code='ARCHIVE_INTEGRITY' AND x.cleanup_not_before<=clock_timestamp()
 AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp()) AND t.status='active'
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 ORDER BY x.cleanup_not_before,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.v30_archive_uploads SET cleanup_token=gen_random_uuid(),cleanup_lease_until=clock_timestamp()+interval '2 minutes'
 WHERE tenant_id=d.tenant_id AND id=d.id RETURNING * INTO d;
 RETURN NEXT d;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.renew_v30_archive_cleanup(p_tenant uuid,p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_uploads x SET cleanup_lease_until=clock_timestamp()+interval '2 minutes'
 WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.state='aborting' AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp()
 AND EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)));
 RETURN FOUND;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.release_v30_archive_cleanup(p_tenant uuid,p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_uploads SET cleanup_token=NULL,cleanup_lease_until=NULL
 WHERE tenant_id=p_tenant AND id=p_upload AND state='aborting' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
-- Trusted scheduler calls only after exact-key physical purge and observation.
CREATE FUNCTION photo.finish_v30_archive_cleanup(p_tenant uuid,p_upload uuid,p_token uuid,p_bucket text,p_key text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.cleanup_token IS DISTINCT FROM p_token OR d.bucket IS DISTINCT FROM p_bucket OR d.object_key IS DISTINCT FROM p_key THEN RETURN false;END IF;
 IF d.state='aborted' THEN RETURN true;END IF;
 IF d.state<>'aborting' OR d.error_code IS DISTINCT FROM 'ARCHIVE_INTEGRITY' OR d.cleanup_not_before IS NULL OR d.cleanup_not_before>clock_timestamp()
 OR d.cleanup_lease_until IS NULL OR d.cleanup_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id=d.artifact_id OR (a.bucket=d.bucket AND a.object_key=d.object_key))) THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.expected_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.expected_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'ARCHIVE_CLEANUP_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v30_archive_uploads SET state='aborted',cleanup_lease_until=NULL WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_v30_archive_completion(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_v30_archive_completion(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.fail_v30_archive_completion(uuid,uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.claim_v30_archive_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_archive_cleanup() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_v30_archive_cleanup() TO photo_scheduler;
ALTER FUNCTION photo.renew_v30_archive_cleanup(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_v30_archive_cleanup(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.renew_v30_archive_cleanup(uuid,uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.release_v30_archive_cleanup(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v30_archive_cleanup(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.release_v30_archive_cleanup(uuid,uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_cleanup(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
