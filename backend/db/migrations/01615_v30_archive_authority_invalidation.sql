-- +goose Up
-- Cancellation of a queued Complete whose original authority disappeared.
-- No physical acknowledgement or quota release is inferred from this transition.
-- +goose StatementBegin
CREATE FUNCTION photo.invalidate_v30_archive_authority() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state='completing' AND t.status='active'
 AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp())
 AND (photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,x.completion_session_id) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=x.tenant_id AND p.id=x.project_id AND p.archived_at IS NULL AND p.download_allowed))
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 ORDER BY x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.v30_archive_uploads SET state='aborting',error_code='UPLOAD_ABORTED',completion_token=NULL,completion_lease_until=NULL,
 cleanup_not_before=greatest(expires_at,coalesce(parts_authorized_until,expires_at),clock_timestamp())+interval '2 hours 5 minutes'
 WHERE tenant_id=d.tenant_id AND id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.invalidate_v30_archive_authority() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.invalidate_v30_archive_authority() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.invalidate_v30_archive_authority() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
