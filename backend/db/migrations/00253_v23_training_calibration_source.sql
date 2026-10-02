-- Pin a training Calibration to a DBA-maintained qualification policy.
-- +goose Up
ALTER TABLE photo.v2_qualification_policies
 ADD COLUMN training_calibration_sha text
 CHECK (training_calibration_sha ~ '^[0-9a-f]{64}$');

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_calibration_source(
 p_tenant uuid,p_project uuid,p_artifact uuid,p_sha text,
 p_model_lock text,p_actor uuid,p_auth_session uuid)
RETURNS TABLE(dataset_sha text,bucket text,object_key text,
 object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_artifact IS NULL OR
  p_actor IS NULL OR p_auth_session IS NULL OR
  coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_model_lock,'') !~ '^[0-9a-f]{64}$' OR
  NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session)
  THEN RAISE EXCEPTION 'V23_TRAIN_CALIBRATION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 RETURN QUERY SELECT policy.dataset_sha,a.bucket,a.object_key,
  a.object_version,a.bytes,a.media_type
 FROM photo.v2_qualification_policies policy
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=p_artifact
  AND a.sha256=p_sha
 WHERE policy.operation='session.describe' AND policy.operation_version='1'
  AND policy.model_lock_sha=p_model_lock AND policy.training_calibration_sha=p_sha
  AND policy.admission_enabled AND policy.enabled AND policy.license_cleared
  AND a.state='ready' AND a.media_type='application/json'
  AND a.bytes BETWEEN 2 AND 1048576 AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
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
ALTER FUNCTION photo.v23_training_calibration_source(
 uuid,uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_calibration_source(
 uuid,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_calibration_source(
 uuid,uuid,uuid,text,text,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
