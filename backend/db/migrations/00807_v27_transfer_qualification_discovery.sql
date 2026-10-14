-- +goose Up
-- Bounded locator discovery; original locked policy gate remains authoritative.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_transfer_qualification_candidates(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected jsonb)
RETURNS SETOF jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate jsonb;
BEGIN
 IF jsonb_typeof(p_expected) IS DISTINCT FROM 'object' OR pg_column_size(p_expected)>4194304 OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 OR p_expected->>'Operation' IS DISTINCT FROM 'semantic.transfer' OR p_expected->>'Version' IS DISTINCT FROM '1' OR p_expected->>'Backend' IS DISTINCT FROM 'cuda' THEN RETURN;END IF;
 FOR candidate IN
 SELECT jsonb_build_object('policy_id',p.id,'qualification_id',q.id,'policy_sha',photo.v27_semantic_policy_sha(p.id),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256),'calibration',jsonb_build_object('artifact_id',calibration.id,'sha256',calibration.sha256),
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'backend',p.backend)
 FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project AND q.operation=p.operation AND q.operation_version=p.operation_version
 AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha AND q.backend=p.backend AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant AND calibration.sha256=ANY(p.calibration_shas)
 WHERE p.operation='semantic.transfer' AND p.operation_version='1' AND p.backend='cuda' AND p.enabled AND p.admission_enabled AND p.license_cleared
 AND p.model_lock_sha=p_expected->>'ModelLockSHA' AND p.runtime_lock_sha=p_expected->>'RuntimeLockSHA'
 AND p.device_profile_sha=p_expected->>'DeviceProfileSHA' AND p.dataset_sha=p_expected->>'DatasetSHA' AND p.issuer_id::text=p_expected->>'IssuerID'
 AND q.trusted_import AND q.status='passed'
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND calibration.state='ready' AND calibration.verified_at IS NOT NULL AND calibration.media_type='application/json' AND calibration.bytes BETWEEN 2 AND 1048576
 AND report.id<>calibration.id AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
 AND calibration.object_version IS NOT NULL AND calibration.object_version NOT IN('','null')
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=p.id::text)
 AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp()) AND (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp())
 ORDER BY p.id,q.id,calibration.id LIMIT 2
 LOOP
 IF photo.lock_semantic_proposal_qualification(p_tenant,p_project,p_actor,p_session,candidate) IS TRUE THEN RETURN NEXT candidate;END IF;
 END LOOP;
END $$;
ALTER FUNCTION photo.semantic_proposal_transfer_qualification_candidates(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_transfer_qualification_candidates(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_transfer_qualification_candidates(uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
