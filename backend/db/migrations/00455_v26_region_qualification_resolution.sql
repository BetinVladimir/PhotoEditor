-- +goose Up
-- Optional DBA pins keep Region unavailable until its complete installed
-- calibration and output policy are independently approved. No activation here.
ALTER TABLE photo.v2_qualification_policies
 ADD COLUMN region_icc_lock_sha text CHECK (region_icc_lock_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN region_execution_policy_sha text CHECK (region_execution_policy_sha ~ '^[0-9a-f]{64}$');
-- +goose StatementBegin
CREATE FUNCTION photo.resolve_v26_region_qualification(
 p_tenant uuid,p_project uuid,p_calibration_sha text,p_evidence_sha text)
RETURNS TABLE(runtime_lock_sha text,model_lock_sha text,icc_lock_sha text,
 device_profile_sha text,dataset_sha text,policy_sha text,backend text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT policy.runtime_lock_sha,policy.model_lock_sha,policy.region_icc_lock_sha,
  policy.device_profile_sha,policy.dataset_sha,policy.region_execution_policy_sha,policy.backend
 FROM photo.v2_qualification_policies policy
 WHERE p_tenant IS NOT NULL AND p_project IS NOT NULL
  AND nullif(current_setting('app.tenant_id',true),'')::uuid=p_tenant
  AND p_calibration_sha ~ '^[0-9a-f]{64}$' AND p_evidence_sha ~ '^[0-9a-f]{64}$'
  AND p_calibration_sha=ANY(policy.calibration_shas)
  AND p_evidence_sha=ANY(policy.evidence_shas)
  AND policy.region_icc_lock_sha IS NOT NULL AND policy.region_execution_policy_sha IS NOT NULL
  AND policy.operation='region.preview' AND policy.operation_version='1' AND policy.backend='cuda'
  AND policy.admission_enabled AND policy.enabled AND policy.license_cleared
  AND EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=policy.operation
   AND op.version=policy.operation_version AND op.v2_enabled AND op.backends ? 'cuda')
  AND EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant AND tenant.status='active')
  AND EXISTS(SELECT 1 FROM photo.projects project WHERE project.tenant_id=p_tenant
   AND project.id=p_project AND project.archived_at IS NULL)
  AND EXISTS(SELECT 1 FROM photo.model_qualifications qualified
   JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id
    AND report.id=qualified.report_artifact_id AND report.sha256=qualified.report_sha
   WHERE qualified.tenant_id=p_tenant AND qualified.project_id=p_project
    AND qualified.operation=policy.operation AND qualified.operation_version=policy.operation_version
    AND qualified.model_lock_sha=policy.model_lock_sha AND qualified.runtime_lock_sha=policy.runtime_lock_sha
    AND qualified.backend=policy.backend AND qualified.device_profile_sha=policy.device_profile_sha
    AND qualified.dataset_sha=policy.dataset_sha AND qualified.trusted_import AND qualified.status='passed'
    AND report.state='ready' AND report.media_type='application/json' AND report.verified_at IS NOT NULL
    AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
    AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp()))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.resolve_v26_region_qualification(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.resolve_v26_region_qualification(uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.resolve_v26_region_qualification(uuid,uuid,text,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
