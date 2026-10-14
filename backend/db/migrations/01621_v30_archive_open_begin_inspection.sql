-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.inspect_v30_archive_open_begin(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;writer uuid;writer_count bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.expires_at<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;

 IF d.state<>'reserved' THEN RETURN NULL;END IF;
 SELECT count(*) INTO writer_count FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND kind='begin';
 IF writer_count<>1 THEN RETURN NULL;END IF;
 SELECT token INTO writer FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND kind='begin' AND state='open' FOR SHARE;
 RETURN writer;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.inspect_v30_archive_open_begin(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_v30_archive_open_begin(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.inspect_v30_archive_open_begin(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
