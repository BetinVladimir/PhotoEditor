-- +goose Up
-- Current actor/project authorization is required even for failed/expired rows.
-- This function never creates a reservation or modifies quota.
-- +goose StatementBegin
CREATE FUNCTION photo.lookup_v30_archive_upload(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_upload uuid)
RETURNS TABLE(request_body text,request_sha text,key text,token uuid,state text,multipart_id text,bucket text,object_key text,artifact_id uuid,expires_at timestamptz,error_code text,archive_ready boolean)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.request_body,u.request_sha,u.key,u.token,u.state,u.multipart_id,u.bucket,u.object_key,u.artifact_id,u.expires_at,u.error_code,
 photo.v30_uploaded_archive_ready(p_tenant,p_project,p_actor,p_session,u.artifact_id,u.expected_sha)
 FROM photo.v30_archive_uploads u JOIN photo.projects p ON p.tenant_id=u.tenant_id AND p.id=u.project_id JOIN photo.tenants t ON t.id=u.tenant_id
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 AND u.tenant_id=p_tenant AND u.project_id=p_project AND u.actor_id=p_actor AND u.id=p_upload
 AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session);
$$;
ALTER FUNCTION photo.lookup_v30_archive_upload(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lookup_v30_archive_upload(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.lookup_v30_archive_upload(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
