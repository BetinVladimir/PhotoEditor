-- +goose Up
-- Parent cleanup may use a generation key only after that generation's exact
-- physical purge was recorded. It never treats an empty S3 listing as proof
-- that an unknown Create request has terminated.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_cleanup_key_current(p_tenant uuid,p_upload uuid,p_key text,p_multipart text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT NULL AND p_upload IS NOT NULL AND p_key IS NOT NULL AND
 ((p_key='tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/archive.zip'
   AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND g.state<>'aborted'))
 OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g
   WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND g.state='aborted' AND g.purged_at IS NOT NULL
   AND g.object_key=p_key AND g.multipart_id=p_multipart
   AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations other
    WHERE other.tenant_id=p_tenant AND other.upload_id=p_upload AND other.state<>'aborted')));
$$;
ALTER FUNCTION photo.v30_archive_cleanup_key_current(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_cleanup_key_current(uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;
BEGIN
 SELECT pg_get_functiondef('photo.v30_archive_cleanup_authorized(uuid,uuid,uuid,text,text)'::regprocedure) INTO definition;
 needle:='x.object_key=''tenants/''||p_tenant||''/interchange-uploads/''||p_upload||''/archive.zip''';
 IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_cleanup_key_current' IN definition)>0
 THEN RAISE EXCEPTION 'ARCHIVE_PARENT_CLEANUP_KEY_SHAPE';END IF;
 EXECUTE replace(definition,needle,'photo.v30_archive_cleanup_key_current(p_tenant,p_upload,x.object_key,x.multipart_id)');
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
