-- +goose Up
-- A workflow may have more than 500 decisions. Keep revocation discovery
-- available with a stable keyset cursor under the existing editor ACL.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_review_saved_page(
 p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_workflow uuid,
 p_after_entry uuid,p_after_target uuid,p_after_kind text,p_after_proposal uuid)
RETURNS SETOF jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL OR p_workflow IS NULL
 OR ((p_after_entry IS NULL OR p_after_target IS NULL OR p_after_kind IS NULL OR p_after_proposal IS NULL)
     AND (p_after_entry IS NOT NULL OR p_after_target IS NOT NULL OR p_after_kind IS NOT NULL OR p_after_proposal IS NOT NULL))
 OR (p_after_kind IS NOT NULL AND p_after_kind NOT IN('defects','dust')) THEN RETURN;END IF;
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
  AND d.project_id=p_project AND (p_after_entry IS NULL OR
   (d.entry_id,d.target_version_id,d.kind,d.proposal_id)>(p_after_entry,p_after_target,p_after_kind,p_after_proposal))
 ORDER BY d.entry_id,d.target_version_id,d.kind,d.proposal_id LIMIT 501;
END $$;
ALTER FUNCTION photo.v30_import_review_saved_page(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_review_saved_page(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_review_saved_page(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
