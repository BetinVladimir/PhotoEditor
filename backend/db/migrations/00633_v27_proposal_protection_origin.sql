-- +goose Up
-- Pre-run editor/version boundary around existing private canonical producer proof.
-- Exact MaskRef bytes, geometry, trusted selection and pixels are separate checks.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_protection_origin(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_version uuid,p_source uuid,p_source_sha text,p_root uuid,p_root_sha text,p_child uuid,p_child_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.tenants t ON t.id=v.tenant_id AND t.status='active'
 JOIN photo.projects project ON project.tenant_id=a.tenant_id AND project.id=a.project_id AND project.archived_at IS NULL
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.review_state='accepted'
 AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
 AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha)
 AND photo.semantic_protection_producer_ready(p_tenant,p_project,p_source,p_source_sha,p_root,p_root_sha,p_child,p_child_sha),false);
$$;
ALTER FUNCTION photo.semantic_proposal_protection_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_protection_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_protection_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
