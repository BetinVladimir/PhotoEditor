-- Resolve the single DBA-selected session descriptor model for training.
-- The client supplies only a calibration ref; the model lock comes from the
-- active qualification policy and trusted report for this project.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_model_lock(p_tenant uuid,p_project uuid,
 p_calibration_sha text,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(model_lock_sha text,dataset_sha text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR
  coalesce(p_calibration_sha,'') !~ '^[0-9a-f]{64}$' OR
  NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session)
 THEN RAISE EXCEPTION 'V23_TRAIN_MODEL_LOCK_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 RETURN QUERY SELECT policy.model_lock_sha,policy.dataset_sha
 FROM photo.v2_qualification_policies policy
 WHERE policy.operation='session.describe' AND policy.operation_version='1'
  AND policy.training_calibration_sha=p_calibration_sha
  AND policy.admission_enabled AND policy.enabled AND policy.license_cleared
  AND EXISTS(SELECT 1 FROM photo.model_qualifications qualified
   JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id
    AND report.id=qualified.report_artifact_id
    AND report.sha256=qualified.report_sha
   WHERE qualified.tenant_id=p_tenant AND qualified.project_id=p_project
    AND qualified.operation=policy.operation
    AND qualified.operation_version=policy.operation_version
    AND qualified.model_lock_sha=policy.model_lock_sha
    AND qualified.runtime_lock_sha=policy.runtime_lock_sha
    AND qualified.backend=policy.backend
    AND qualified.device_profile_sha=policy.device_profile_sha
    AND qualified.dataset_sha=policy.dataset_sha
    AND qualified.trusted_import AND qualified.status='passed'
    AND report.state='ready' AND report.media_type='application/json'
    AND report.verified_at IS NOT NULL AND report.object_version IS NOT NULL
    AND report.object_version NOT IN ('','null')
    AND (report.expires_at IS NULL OR
     report.expires_at>clock_timestamp()));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_model_lock(uuid,uuid,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_model_lock(uuid,uuid,text,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_model_lock(uuid,uuid,text,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
