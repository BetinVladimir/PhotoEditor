-- +goose Up
-- Only a bound generation may replace the legacy archive object key.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_storage_key_current(p_tenant uuid,p_upload uuid,p_key text,p_multipart text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT NULL AND p_upload IS NOT NULL AND p_key IS NOT NULL AND
 ((p_key='tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/archive.zip'
   AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND g.state='bound'))
 OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g JOIN photo.v30_archive_writers w
   ON w.tenant_id=g.tenant_id AND w.upload_id=g.upload_id AND w.token=g.writer_token
   WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND g.state='bound' AND g.object_key=p_key
   AND g.multipart_id=p_multipart AND w.kind='begin' AND w.state='closed' AND w.receipt=p_multipart));
$$;
ALTER FUNCTION photo.v30_archive_storage_key_current(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_storage_key_current(uuid,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lookup_v30_archive_upload(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_upload uuid)
RETURNS TABLE(request_body text,request_sha text,key text,token uuid,state text,multipart_id text,bucket text,object_key text,artifact_id uuid,expires_at timestamptz,error_code text,archive_ready boolean)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.request_body,u.request_sha,u.key,u.token,u.state,u.multipart_id,u.bucket,u.object_key,u.artifact_id,u.expires_at,u.error_code,
 photo.v30_uploaded_archive_ready(p_tenant,p_project,p_actor,p_session,u.artifact_id,u.expected_sha)
 FROM photo.v30_archive_uploads u JOIN photo.projects p ON p.tenant_id=u.tenant_id AND p.id=u.project_id JOIN photo.tenants t ON t.id=u.tenant_id
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 AND u.tenant_id=p_tenant AND u.project_id=p_project AND u.actor_id=p_actor AND u.id=p_upload
 AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND photo.v30_archive_storage_key_current(u.tenant_id,u.id,u.object_key,u.multipart_id);
$$;
-- +goose StatementEnd

-- The existing part authorizer still controls actor/session, expiry, size and
-- signing horizon. Add exact storage-key authority before it records URLs.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text:=' total:=(d.expected_bytes+16777215)/16777216;';
BEGIN
 SELECT pg_get_functiondef('photo.authorize_v30_archive_parts(uuid,uuid,uuid,uuid,uuid,text,integer[])'::regprocedure) INTO definition;
 IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_storage_key_current' IN definition)>0
 THEN RAISE EXCEPTION 'ARCHIVE_PARTS_GENERATION_KEY_SHAPE';END IF;
 EXECUTE replace(definition,needle,
  ' IF photo.v30_archive_storage_key_current(p_tenant,p_upload,d.object_key,d.multipart_id) IS DISTINCT FROM true THEN RETURN;END IF;'||needle);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
