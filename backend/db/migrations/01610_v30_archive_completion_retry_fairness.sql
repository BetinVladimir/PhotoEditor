-- +goose Up
ALTER TABLE photo.v30_archive_uploads ADD COLUMN completion_retry_at timestamptz NOT NULL DEFAULT clock_timestamp();
-- Open writers are reconciled separately; ordinary completion must not spin on
-- the oldest ambiguous upload and starve other tenants/uploads.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_v30_archive_completion() RETURNS SETOF photo.v30_archive_uploads
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id JOIN photo.projects p ON p.tenant_id=x.tenant_id AND p.id=x.project_id
 WHERE x.state='completing' AND x.expires_at>clock_timestamp() AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp())
 AND x.completion_retry_at<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state='open')
 AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed
 AND photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,x.completion_session_id)
 ORDER BY x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.v30_archive_uploads SET completion_token=fence,completion_lease_until=clock_timestamp()+interval '2 minutes' WHERE tenant_id=d.tenant_id AND id=d.id RETURNING * INTO d;
 RETURN NEXT d;
END $$;
-- +goose StatementEnd
-- Transient IO failure retains parts/closed receipts but schedules a bounded
-- retry delay. Caller cancellation uses the same release path.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.release_v30_archive_completion(p_tenant uuid,p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_uploads SET completion_token=NULL,completion_lease_until=NULL,completion_retry_at=clock_timestamp()+interval '30 seconds' WHERE tenant_id=p_tenant AND id=p_upload AND state='completing' AND completion_token=p_token AND completion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
