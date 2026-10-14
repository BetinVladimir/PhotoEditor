-- +goose Up
CREATE TABLE photo.v30_archive_writers(
 tenant_id uuid NOT NULL,upload_id uuid NOT NULL,token uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('begin','complete')),state text NOT NULL DEFAULT 'open' CHECK(state IN('open','closed')),
 receipt text,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),closed_at timestamptz,
 PRIMARY KEY(tenant_id,upload_id,token),FOREIGN KEY(tenant_id,upload_id) REFERENCES photo.v30_archive_uploads(tenant_id,id),
 CHECK((state='open' AND receipt IS NULL AND closed_at IS NULL) OR (state='closed' AND length(receipt) BETWEEN 1 AND 2048 AND closed_at IS NOT NULL))
);
ALTER TABLE photo.v30_archive_writers ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_archive_writers FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_archive_writers TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v30_archive_writers TO photo_fn_owner;
REVOKE ALL ON photo.v30_archive_writers FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
CREATE INDEX ON photo.v30_archive_writers(tenant_id,upload_id) WHERE state='open';
-- Open writer tickets NEVER expire automatically: a lost process or ambiguous
-- remote response requires explicit reconciliation, not timer-based deletion.
-- +goose StatementBegin
CREATE FUNCTION photo.start_v30_archive_begin(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;ticket uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.state<>'reserved' OR d.expires_at<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND state='open') THEN RETURN NULL;END IF;
 INSERT INTO photo.v30_archive_writers(tenant_id,upload_id,token,kind) VALUES(p_tenant,p_upload,ticket,'begin');RETURN ticket;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.start_v30_archive_complete(p_tenant uuid,p_upload uuid,p_completion uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;ticket uuid:=gen_random_uuid();
BEGIN
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.state<>'completing' OR d.completion_token IS DISTINCT FROM p_completion OR d.completion_lease_until IS NULL
 OR d.completion_lease_until<=clock_timestamp() OR d.expires_at<=clock_timestamp() THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND state='open') THEN RETURN NULL;END IF;
 INSERT INTO photo.v30_archive_writers(tenant_id,upload_id,token,kind) VALUES(p_tenant,p_upload,ticket,'complete');RETURN ticket;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_begin_writer(p_tenant uuid,p_upload uuid,p_token uuid,p_receipt text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL OR p_receipt IS NULL OR length(p_receipt) NOT BETWEEN 1 AND 2048 THEN RETURN false;END IF;
 UPDATE photo.v30_archive_writers SET state='closed',receipt=p_receipt,closed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_token AND kind='begin' AND state='open';
 IF FOUND THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_token AND kind='begin' AND state='closed' AND receipt=p_receipt);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_complete_writer(p_tenant uuid,p_upload uuid,p_token uuid,p_receipt text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL OR p_receipt IS NULL OR length(p_receipt) NOT BETWEEN 1 AND 2048 THEN RETURN false;END IF;
 UPDATE photo.v30_archive_writers SET state='closed',receipt=p_receipt,closed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_token AND kind='complete' AND state='open';
 IF FOUND THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_token AND kind='complete' AND state='closed' AND receipt=p_receipt);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.start_v30_archive_begin(uuid,uuid,uuid,uuid,uuid) TO photo_api;
ALTER FUNCTION photo.start_v30_archive_complete(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.start_v30_archive_complete(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.start_v30_archive_complete(uuid,uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.finish_v30_archive_begin_writer(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_begin_writer(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_begin_writer(uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_complete_writer(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_archive_cleanup_authorized(p_tenant uuid,p_upload uuid,p_token uuid,p_bucket text,p_key text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_token IS NOT NULL AND EXISTS(
 SELECT 1 FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp()
 AND x.state='aborting' AND x.error_code IN('ARCHIVE_INTEGRITY','UPLOAD_EXPIRED','UPLOAD_ABORTED') AND t.status='active'
 AND x.bucket=p_bucket AND x.object_key=p_key AND x.object_key='tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/archive.zip'
 AND x.cleanup_not_before<=clock_timestamp() AND x.expires_at<=clock_timestamp()
 AND (x.parts_authorized_until IS NULL OR x.parts_authorized_until<=clock_timestamp()) AND x.completion_lease_until IS NULL
 AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=x.tenant_id AND w.upload_id=x.id AND w.state='open')
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 );
$$;
ALTER FUNCTION photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE definition text; BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0 THEN RAISE EXCEPTION 'PURGE_ALLOWLIST_NOT_FOUND';END IF;
 EXECUTE replace(definition,'tables_to_purge constant text[]:=ARRAY[','tables_to_purge constant text[]:=ARRAY[''v30_archive_writers'',');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
