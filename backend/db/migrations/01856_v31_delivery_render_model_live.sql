-- +goose Up
-- Recheck the editor's live model/consent conditions without an editor
-- session, anchored to the frozen Params of one adopted delegated render task.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_model_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_model uuid,p_model_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;
BEGIN
 IF p_model_sha!~'^[0-9a-f]{64}$' THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.v2_documents d ON d.tenant_id=root.tenant_id
  AND d.project_id=root.project_id AND d.sha256=root.render_authored_sha
 JOIN photo.v2_document_streams stream ON stream.tenant_id=d.tenant_id
  AND stream.id=d.stream_id
 JOIN photo.model_use_links used ON used.tenant_id=d.tenant_id
  AND used.project_id=d.project_id AND used.document_id=d.id
 JOIN photo.learning_versions version ON version.tenant_id=used.tenant_id
  AND version.project_id=used.project_id AND version.id=used.version_id
 JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
  AND profile.project_id=version.project_id AND profile.id=version.profile_id
 JOIN photo.version_eligibility eligibility ON eligibility.tenant_id=version.tenant_id
  AND eligibility.project_id=version.project_id AND eligibility.version_id=version.id
 JOIN photo.artifacts model ON model.tenant_id=version.tenant_id
  AND model.id=version.model_artifact_id
 JOIN photo.artifacts training ON training.tenant_id=version.tenant_id
  AND training.id=version.training_artifact_id
 WHERE root.tenant_id=(source->>'tenant_id')::uuid
  AND root.project_id=(source->>'project_id')::uuid
  AND root.artifact_id=(e->'params'->>'artifact_id')::uuid
  AND root.sha256=e->'params'->>'sha256'
  AND root.state='ready' AND root.render_registered_at IS NOT NULL
  AND root.render_learning_checked AND root.render_learning IS NOT NULL
  AND root.render_learning->>'version_id'=version.id::text
  AND root.render_learning->'model'->>'artifact_id'=p_model::text
  AND root.render_learning->'model'->>'sha256'=p_model_sha
  AND d.document_type='EditingRecipeV3'
  AND stream.project_id=root.project_id
  AND stream.asset_version_id=root.asset_version_id
  AND stream.stream_type='draft'
  AND version.id=used.version_id AND profile.active_version_id=version.id
  AND eligibility.status='eligible'
  AND model.id=p_model AND model.sha256=p_model_sha
  AND model.state='ready' AND model.output_role='document'
  AND model.media_type='application/json' AND model.verified_at IS NOT NULL
  AND model.object_version NOT IN('','null')
  AND (model.expires_at IS NULL OR model.expires_at>clock_timestamp())
  AND training.state='ready'
  AND training.output_role IN('document','v23_training_manifest')
  AND training.media_type='application/json' AND training.verified_at IS NOT NULL
  AND training.object_version NOT IN('','null')
  AND (training.expires_at IS NULL OR training.expires_at>clock_timestamp())
  AND photo.v23_learning_version_consents_live(root.tenant_id,version.id));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_model_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_model_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_model_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
