-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.abort_v30_archive_upload(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_request_sha text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT x.* INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.actor_id=p_actor FOR UPDATE;
 IF NOT FOUND OR d.request_sha IS DISTINCT FROM p_request_sha OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF d.state IN('aborting','aborted') THEN RETURN true;END IF;
 IF d.state NOT IN('reserved','uploading','completing') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id=d.artifact_id OR (a.bucket=d.bucket AND a.object_key=d.object_key))) THEN RETURN false;END IF;
 UPDATE photo.v30_archive_uploads SET state='aborting',error_code='UPLOAD_ABORTED',completion_token=NULL,completion_lease_until=NULL,
 cleanup_not_before=greatest(expires_at,coalesce(parts_authorized_until,expires_at),clock_timestamp())+interval '2 hours 5 minutes'
 WHERE tenant_id=p_tenant AND id=p_upload;
 -- State fences signing/adoption; cleanup must independently attest drained IO.
 RETURN true;
END $$;
ALTER FUNCTION photo.abort_v30_archive_upload(uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.abort_v30_archive_upload(uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.abort_v30_archive_upload(uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_v30_archive_cleanup() RETURNS SETOF photo.v30_archive_uploads
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED') AND x.cleanup_not_before<=clock_timestamp()
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
CREATE OR REPLACE FUNCTION photo.finish_v30_archive_cleanup(p_tenant uuid,p_upload uuid,p_token uuid,p_bucket text,p_key text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.cleanup_token IS DISTINCT FROM p_token OR d.bucket IS DISTINCT FROM p_bucket OR d.object_key IS DISTINCT FROM p_key THEN RETURN false;END IF;
 IF d.state='aborted' THEN RETURN true;END IF;
 IF d.state<>'aborting' OR (d.error_code IS NULL OR d.error_code NOT IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED')) OR d.cleanup_not_before IS NULL OR d.cleanup_not_before>clock_timestamp()
 OR d.cleanup_lease_until IS NULL OR d.cleanup_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id=d.artifact_id OR (a.bucket=d.bucket AND a.object_key=d.object_key))) THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.expected_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.expected_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'ARCHIVE_CLEANUP_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v30_archive_uploads SET state='aborted',cleanup_lease_until=NULL WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
