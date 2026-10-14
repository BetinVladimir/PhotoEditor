-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.recover_v30_archive_begin(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;multipart text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.expires_at<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 IF d.state='uploading' THEN RETURN d.multipart_id;END IF;
 IF d.state<>'reserved' OR EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND state='open') THEN RETURN NULL;END IF;
 SELECT receipt INTO multipart FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND kind='begin' AND state='closed' ORDER BY created_at,token LIMIT 1;
 IF multipart IS NULL OR length(multipart) NOT BETWEEN 1 AND 1024 OR multipart ~ '[[:cntrl:]]' THEN RETURN NULL;END IF;
 UPDATE photo.v30_archive_uploads SET state='uploading',multipart_id=multipart WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN multipart;
END $$;
ALTER FUNCTION photo.recover_v30_archive_begin(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_v30_archive_begin(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.recover_v30_archive_begin(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.start_v30_archive_begin(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;ticket uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.state<>'reserved' OR d.expires_at<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND (state='open' OR kind='begin')) THEN RETURN NULL;END IF;
 INSERT INTO photo.v30_archive_writers(tenant_id,upload_id,token,kind) VALUES(p_tenant,p_upload,ticket,'begin');RETURN ticket;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
