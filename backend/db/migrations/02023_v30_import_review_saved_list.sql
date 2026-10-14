-- +goose Up
-- Saved decisions remain visible for revocation after the import run stops.
-- No archived bundle or prepared proposal is read by this catalogue.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_review_saved_list(
 p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_workflow uuid)
RETURNS SETOF jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL OR p_workflow IS NULL THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows x WHERE x.tenant_id=p_tenant AND x.id=p_workflow
  AND x.project_id=p_project AND x.workflow_type='interchange' FOR SHARE;
 IF NOT FOUND OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT jsonb_build_object('entry_id',d.entry_id,'target_version_id',d.target_version_id,
  'kind',d.kind,'proposal',jsonb_build_object('artifact_id',d.proposal_id,'sha256',d.proposal_sha),
  'base',jsonb_build_object('artifact_id',d.base_id,'sha256',d.base_sha),
  'label_map',CASE WHEN d.label_map_id IS NULL THEN NULL ELSE jsonb_build_object('artifact_id',d.label_map_id,'sha256',d.label_map_sha) END,
  'decision',d.decision,'accepted_candidate_ids',d.accepted_ids,'rejected_candidate_ids',d.rejected_ids,
  'status',d.status,'epoch',d.epoch)
 FROM photo.v30_import_review_decisions d WHERE d.tenant_id=p_tenant AND d.workflow_id=p_workflow
  AND d.project_id=p_project ORDER BY d.entry_id,d.target_version_id,d.kind,d.proposal_id LIMIT 501;
END $$;
ALTER FUNCTION photo.v30_import_review_saved_list(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_review_saved_list(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_review_saved_list(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
