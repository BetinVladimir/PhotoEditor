-- Resolve the active version only while compiling a missing training draft.
-- The caller freezes its exact artifact ref in the manifest; pending replay
-- must use that frozen ref instead of selecting the current active version.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_active_baseline(
 p_tenant uuid,p_project uuid,p_profile uuid,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(has_active boolean,version_id uuid,training_sha text,artifact_id uuid,
 artifact_sha text,bucket text,object_key text,object_version text,
 bytes bigint,media_type text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_profile IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR
  NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session)
 THEN RAISE EXCEPTION 'V23_TRAIN_BASELINE_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 RETURN QUERY SELECT p.active_version_id IS NOT NULL,v.id,
  coalesce(training.sha256,''),model.id,coalesce(model.sha256,''),
  coalesce(model.bucket,''),coalesce(model.object_key,''),
  coalesce(model.object_version,''),coalesce(model.bytes,0),
  coalesce(model.media_type,'')
 FROM photo.learning_profiles p
 LEFT JOIN photo.learning_versions v ON v.tenant_id=p.tenant_id
  AND v.project_id=p.project_id AND v.profile_id=p.id
  AND v.id=p.active_version_id
 LEFT JOIN photo.version_eligibility eligibility ON
  eligibility.tenant_id=v.tenant_id AND
  eligibility.project_id=v.project_id AND eligibility.version_id=v.id
  AND eligibility.status='eligible'
 LEFT JOIN photo.artifacts training ON training.tenant_id=v.tenant_id
  AND training.id=v.training_artifact_id
  AND training.state='ready' AND training.verified_at IS NOT NULL
  AND training.object_version IS NOT NULL
  AND training.object_version NOT IN ('','null')
  AND (training.expires_at IS NULL OR training.expires_at>clock_timestamp())
 LEFT JOIN photo.artifacts model ON model.tenant_id=v.tenant_id
  AND model.id=v.model_artifact_id AND model.state='ready'
  AND model.media_type='application/json'
  AND model.bytes BETWEEN 2 AND 8388608
  AND model.verified_at IS NOT NULL AND model.object_version IS NOT NULL
  AND model.object_version NOT IN ('','null')
  AND (model.expires_at IS NULL OR model.expires_at>clock_timestamp())
 WHERE p.tenant_id=p_tenant AND p.project_id=p_project AND p.id=p_profile
  AND (p.active_version_id IS NULL OR eligibility.version_id IS NOT NULL);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_active_baseline(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_active_baseline(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_active_baseline(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
