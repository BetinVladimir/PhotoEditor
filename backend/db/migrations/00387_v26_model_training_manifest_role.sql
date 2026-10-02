-- +goose Up
-- Accept the actual adopted TrainingManifest producer role and legacy documents.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_model_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_root uuid,p_root_sha text,p_model uuid,p_model_sha text,p_learning uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v26_render_recipe_scope(p_tenant,p_project,p_version,p_root,p_root_sha,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.v2_documents d ON d.tenant_id=root.tenant_id AND d.project_id=root.project_id AND d.sha256=root.render_authored_sha
 JOIN photo.v2_document_streams stream ON stream.tenant_id=d.tenant_id AND stream.id=d.stream_id
 JOIN photo.model_use_links used ON used.tenant_id=d.tenant_id AND used.project_id=d.project_id AND used.document_id=d.id
 JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id AND version.project_id=used.project_id AND version.id=used.version_id
 JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id AND profile.project_id=version.project_id AND profile.id=version.profile_id
 JOIN photo.version_eligibility eligibility ON eligibility.tenant_id=version.tenant_id AND eligibility.project_id=version.project_id AND eligibility.version_id=version.id
 JOIN photo.artifacts model ON model.tenant_id=version.tenant_id AND model.id=version.model_artifact_id
 JOIN photo.artifacts training ON training.tenant_id=version.tenant_id AND training.id=version.training_artifact_id
 WHERE root.tenant_id=p_tenant AND root.project_id=p_project AND root.asset_version_id=p_version
 AND root.artifact_id=p_root AND root.sha256=p_root_sha AND root.state='ready' AND root.render_registered_at IS NOT NULL
 AND d.document_type='EditingRecipeV3' AND stream.project_id=p_project AND stream.asset_version_id=p_version AND stream.stream_type='draft'
 AND version.id=p_learning AND profile.active_version_id=version.id AND eligibility.status='eligible'
 AND model.id=p_model AND model.sha256=p_model_sha AND model.state='ready' AND model.output_role='document'
 AND model.media_type='application/json' AND model.verified_at IS NOT NULL AND model.object_version IS NOT NULL AND model.object_version NOT IN('','null')
 AND (model.expires_at IS NULL OR model.expires_at>clock_timestamp())
 AND training.state='ready' AND training.output_role IN('document','v23_training_manifest') AND training.media_type='application/json'
 AND training.verified_at IS NOT NULL AND training.object_version IS NOT NULL AND training.object_version NOT IN('','null')
 AND (training.expires_at IS NULL OR training.expires_at>clock_timestamp())
 AND photo.v23_learning_version_consents_live(p_tenant,version.id));
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
