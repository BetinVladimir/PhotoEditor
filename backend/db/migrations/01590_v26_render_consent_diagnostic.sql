-- +goose Up
-- Narrow diagnostic only; never grants artifact access or activation.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_model_consent_revoked(p_tenant uuid,p_project uuid,p_version uuid,p_root uuid,p_root_sha text,p_learning uuid,p_model uuid,p_model_sha text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
SELECT EXISTS(SELECT 1
 FROM photo.v2_draft_uploads root
 JOIN photo.v2_documents d ON d.tenant_id=root.tenant_id AND d.project_id=root.project_id
  AND d.sha256=root.render_authored_sha AND d.document_type='EditingRecipeV3'
 JOIN photo.v2_document_streams stream ON stream.tenant_id=d.tenant_id AND stream.id=d.stream_id
 JOIN photo.model_use_links used ON used.tenant_id=d.tenant_id AND used.project_id=d.project_id AND used.document_id=d.id
 JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id
  AND version.project_id=used.project_id AND version.id=used.version_id
 JOIN photo.artifacts model ON model.tenant_id=version.tenant_id AND model.id=version.model_artifact_id
 WHERE root.tenant_id=p_tenant AND root.project_id=p_project AND root.asset_version_id=p_version
  AND root.artifact_id=p_root AND root.sha256=p_root_sha
  AND stream.project_id=p_project AND stream.asset_version_id=p_version AND stream.stream_type='draft'
  AND version.id=p_learning AND model.id=p_model AND model.sha256=p_model_sha
  AND photo.v26_render_recipe_scope(p_tenant,p_project,p_version,p_root,p_root_sha,p_actor,p_session)
  AND NOT photo.v23_learning_version_consents_live(p_tenant,version.id));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_model_consent_revoked(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_model_consent_revoked(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_model_consent_revoked(uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
