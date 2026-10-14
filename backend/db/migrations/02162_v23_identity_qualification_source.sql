-- +goose Up
-- Scheduler may read only the singular DBA-selected, passed identity policy
-- for an active project and currently registered runtime. No client policy ID.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_qualification_source(p_tenant uuid,
 p_project uuid,p_model text,p_calibration uuid,p_calibration_sha text,
 p_backend text,p_bucket text)
RETURNS TABLE(policy_id uuid,qualification_id uuid,issuer_id uuid,
 model_lock_sha text,runtime_lock_sha text,device_profile_sha text,
 dataset_sha text,backend text,required_checks text[],test_fixture_shas text[],
 calibration_shas text[],evidence_shas text[],report_id uuid,report_sha text,
 object_key text,object_version text,bytes bigint)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
  nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_project IS NULL OR p_calibration IS NULL OR
  p_model !~ '^[0-9a-f]{64}$' OR
  p_calibration_sha !~ '^[0-9a-f]{64}$' OR
  p_backend NOT IN('cpu','cuda') OR length(p_bucket) NOT BETWEEN 3 AND 63
  THEN RETURN;END IF;
 PERFORM t.id FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM p.id FROM photo.projects p WHERE p.tenant_id=p_tenant AND
  p.id=p_project AND p.archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT policy.id,qualified.id,policy.issuer_id,
  policy.model_lock_sha,policy.runtime_lock_sha,policy.device_profile_sha,
  policy.dataset_sha,policy.backend,policy.required_checks,
  policy.test_fixture_shas,policy.calibration_shas,policy.evidence_shas,
  report.id,report.sha256,report.object_key,report.object_version,report.bytes
 FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications qualified ON qualified.tenant_id=p_tenant AND
  qualified.project_id=p_project AND qualified.operation=policy.operation AND
  qualified.operation_version=policy.operation_version AND
  qualified.model_lock_sha=policy.model_lock_sha AND
  qualified.runtime_lock_sha=policy.runtime_lock_sha AND
  qualified.device_profile_sha=policy.device_profile_sha AND
  qualified.dataset_sha=policy.dataset_sha AND
  qualified.backend=policy.backend AND qualified.trusted_import AND
  qualified.status='passed'
 JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id AND
  report.id=qualified.report_artifact_id AND report.sha256=qualified.report_sha AND
  report.bucket=p_bucket AND report.state='ready' AND
  report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608 AND
  report.verified_at IS NOT NULL AND report.object_version IS NOT NULL AND
  report.object_version NOT IN('','null') AND
  (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant AND
  calibration.id=p_calibration AND calibration.sha256=p_calibration_sha AND
  calibration.bucket=p_bucket AND calibration.state='ready' AND
  calibration.media_type='application/json' AND
  calibration.bytes BETWEEN 2 AND 1048576 AND
  calibration.verified_at IS NOT NULL AND calibration.object_version IS NOT NULL AND
  calibration.object_version NOT IN('','null') AND
  (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp())
 WHERE policy.operation='identity.compare' AND policy.operation_version='1' AND
  policy.backend=p_backend AND policy.model_lock_sha=p_model AND
  policy.enabled AND policy.admission_enabled AND policy.license_cleared AND
  p_calibration_sha=ANY(policy.calibration_shas) AND
  (SELECT count(*) FROM photo.model_qualifications candidate WHERE
   candidate.tenant_id=p_tenant AND candidate.project_id=p_project AND
   candidate.operation=policy.operation AND
   candidate.operation_version=policy.operation_version AND
   candidate.model_lock_sha=policy.model_lock_sha AND
   candidate.runtime_lock_sha=policy.runtime_lock_sha AND
   candidate.device_profile_sha=policy.device_profile_sha AND
   candidate.dataset_sha=policy.dataset_sha AND
   candidate.backend=policy.backend AND candidate.trusted_import AND
   candidate.status='passed')=1 AND
  EXISTS(SELECT 1 FROM photo.worker_registrations worker WHERE
   worker.tenant_id=p_tenant AND worker.enabled AND
   worker.last_seen>clock_timestamp()-interval '30 seconds' AND
   worker.resource_classes ? 'gpu' AND worker.backends ? p_backend AND
   (worker.qualified_operations->'identity.compare') ? p_backend AND
   worker.lock_id=policy.runtime_lock_sha)
 FOR SHARE OF policy,qualified,report,calibration;
END $$;
ALTER FUNCTION photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_qualification_source(uuid,uuid,text,uuid,text,text,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
