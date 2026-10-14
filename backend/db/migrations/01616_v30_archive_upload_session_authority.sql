-- +goose Up
ALTER TABLE photo.v30_archive_uploads ADD COLUMN authority_session_id uuid;
-- Legacy pre-Complete sessions cannot be inferred. They remain expiry-managed.
UPDATE photo.v30_archive_uploads SET authority_session_id=completion_session_id WHERE completion_session_id IS NOT NULL;
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 SELECT pg_get_functiondef('photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text)'::regprocedure) INTO definition;
 needle:='artifact_id,bucket,object_key,token)';
 IF position(needle IN definition)=0 OR position('''/archive.zip'',fence);' IN definition)=0 OR position('  RETURN QUERY SELECT ''replayed''' IN definition)=0 THEN RAISE EXCEPTION 'ARCHIVE_SESSION_RESERVE_PATCH_MISMATCH';END IF;
 definition:=replace(definition,needle,'artifact_id,bucket,object_key,token,authority_session_id)');
 definition:=replace(definition,'''/archive.zip'',fence);','''/archive.zip'',fence,p_session);');
 definition:=replace(definition,'  RETURN QUERY SELECT ''replayed''','  IF d.state IN(''reserved'',''uploading'') THEN UPDATE photo.v30_archive_uploads x SET authority_session_id=p_session WHERE x.tenant_id=p_tenant AND x.id=d.id;END IF;
  RETURN QUERY SELECT ''replayed''');
 EXECUTE definition;
 SELECT pg_get_functiondef('photo.bind_v30_archive_multipart(uuid,uuid,uuid,uuid,uuid,text,text)'::regprocedure) INTO definition;
 needle:='SET multipart_id=p_multipart,state=''uploading''';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'ARCHIVE_SESSION_BIND_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,needle||',authority_session_id=p_session');
 SELECT pg_get_functiondef('photo.authorize_v30_archive_parts(uuid,uuid,uuid,uuid,uuid,text,integer[])'::regprocedure) INTO definition;
 needle:='SET parts_authorized_until=';
 IF position(needle IN definition)=0 THEN RAISE EXCEPTION 'ARCHIVE_SESSION_PARTS_PATCH_MISMATCH';END IF;
 EXECUTE replace(definition,needle,'SET authority_session_id=p_session,parts_authorized_until=');
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.invalidate_v30_archive_authority() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id
 WHERE x.state IN('reserved','uploading','completing') AND t.status='active'
 AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp())
 AND ((CASE WHEN x.state='completing' THEN x.completion_session_id ELSE x.authority_session_id END IS NOT NULL
 AND photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,CASE WHEN x.state='completing' THEN x.completion_session_id ELSE x.authority_session_id END) IS DISTINCT FROM true)
 OR NOT EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=x.tenant_id AND p.id=x.project_id AND p.archived_at IS NULL AND p.download_allowed))
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=x.tenant_id AND (a.id=x.artifact_id OR (a.bucket=x.bucket AND a.object_key=x.object_key)))
 ORDER BY x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.v30_archive_uploads SET state='aborting',error_code='UPLOAD_ABORTED',completion_token=NULL,completion_lease_until=NULL,
 cleanup_not_before=greatest(expires_at,coalesce(parts_authorized_until,expires_at),clock_timestamp())+interval '2 hours 5 minutes'
 WHERE tenant_id=d.tenant_id AND id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
