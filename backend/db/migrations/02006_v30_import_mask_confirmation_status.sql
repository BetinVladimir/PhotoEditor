-- +goose Up
-- Read persisted decisions independently of the running import source gate so
-- the editor can revoke a grant after preparation or on reopening the screen.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_person_mask_confirmation_status(
 p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_workflow uuid)
RETURNS TABLE(entry_id uuid,override_id uuid,mask_id uuid,target_version_id uuid,status text,epoch bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL OR p_workflow IS NULL THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND w.project_id=p_project
  AND w.workflow_type='interchange'
  AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS TRUE) THEN RETURN;END IF;
 RETURN QUERY SELECT c.entry_id,c.override_id,c.mask_id,c.target_version_id,c.status,c.epoch
 FROM photo.v30_import_person_mask_confirmations c
 WHERE c.tenant_id=p_tenant AND c.project_id=p_project AND c.workflow_id=p_workflow
 ORDER BY c.entry_id,c.override_id,c.mask_id,c.target_version_id LIMIT 5001;
END $$;
ALTER FUNCTION photo.v30_import_person_mask_confirmation_status(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_person_mask_confirmation_status(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_person_mask_confirmation_status(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
