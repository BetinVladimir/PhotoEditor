-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.recover_v30_archive_complete(p_tenant uuid,p_upload uuid,p_completion uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;version text;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.state<>'completing' OR d.completion_token IS DISTINCT FROM p_completion OR p_completion IS NULL
 OR d.completion_lease_until IS NULL OR d.completion_lease_until<=clock_timestamp() OR d.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND state='open') THEN RETURN NULL;END IF;
 SELECT receipt INTO version FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND kind='complete' AND state='closed' ORDER BY created_at,token LIMIT 1;
 IF version IS NULL OR version='null' OR length(version) NOT BETWEEN 1 AND 1024 OR version ~ '[[:cntrl:]]' THEN RETURN NULL;END IF;
 RETURN version;
END $$;
ALTER FUNCTION photo.recover_v30_archive_complete(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.recover_v30_archive_complete(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.recover_v30_archive_complete(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- Prevent another physical Complete after an observed receipt, even if recovery fails.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.start_v30_archive_complete(p_tenant uuid,p_upload uuid,p_completion uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;ticket uuid:=gen_random_uuid();
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.state<>'completing' OR p_completion IS NULL OR d.completion_token IS DISTINCT FROM p_completion OR d.completion_lease_until IS NULL
 OR d.completion_lease_until<=clock_timestamp() OR d.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND (state='open' OR kind='complete')) THEN RETURN NULL;END IF;
 INSERT INTO photo.v30_archive_writers(tenant_id,upload_id,token,kind) VALUES(p_tenant,p_upload,ticket,'complete');RETURN ticket;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
