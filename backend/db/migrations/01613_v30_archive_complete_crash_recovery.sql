-- +goose Up
-- Recovery adopts a lease, never a new physical writer.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v30_archive_completion_recovery() RETURNS SETOF photo.v30_archive_uploads
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id JOIN photo.projects p ON p.tenant_id=x.tenant_id AND p.id=x.project_id
 WHERE x.state='completing' AND x.expires_at>clock_timestamp() AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp())
 AND x.completion_retry_at<=clock_timestamp()
 AND (SELECT count(*) FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state='open')=1
 AND EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.kind='complete' AND w.state='open')
 AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed
 AND photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,x.completion_session_id)
 ORDER BY x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.v30_archive_uploads SET completion_token=fence,completion_lease_until=clock_timestamp()+interval '2 minutes' WHERE tenant_id=d.tenant_id AND id=d.id RETURNING * INTO d;
 RETURN NEXT d;
END $$;
ALTER FUNCTION photo.claim_v30_archive_completion_recovery() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_archive_completion_recovery() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_v30_archive_completion_recovery() TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v30_archive_complete_writer(p_tenant uuid,p_upload uuid,p_completion uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;writer uuid;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR p_completion IS NULL OR d.completion_token IS DISTINCT FROM p_completion OR d.state<>'completing'
 OR d.expires_at<=clock_timestamp() OR d.completion_lease_until IS NULL OR d.completion_lease_until<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,d.actor_id,d.completion_session_id) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 IF (SELECT count(*) FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND state='open')<>1 THEN RETURN NULL;END IF;
 SELECT token INTO writer FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND kind='complete' AND state='open';
 RETURN writer;
END $$;
ALTER FUNCTION photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.adopt_v30_archive_complete_writer(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
