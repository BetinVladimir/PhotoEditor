-- +goose Up
-- Read-only indication for legacy open Begin writers that predate the
-- generation ledger. An open writer is never terminal evidence for Create.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_open_begin_for_status(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.v30_archive_uploads u
  JOIN photo.projects p ON p.tenant_id=u.tenant_id AND p.id=u.project_id
  JOIN photo.tenants t ON t.id=u.tenant_id
  JOIN photo.v30_archive_writers w ON w.tenant_id=u.tenant_id AND w.upload_id=u.id
  WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
   AND u.tenant_id=p_tenant AND u.project_id=p_project AND u.id=p_upload
   AND u.actor_id=p_actor AND u.token=p_reservation
   AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed
   AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
   AND w.kind='begin' AND w.state='open'
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_archive_open_begin_for_status(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_open_begin_for_status(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_archive_open_begin_for_status(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
