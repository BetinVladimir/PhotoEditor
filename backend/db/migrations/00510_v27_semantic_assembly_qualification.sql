-- +goose Up
-- Resolve the DBA-selected transfer qualification; document qualified=true
-- alone never authorizes semantic compilation.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_qualification(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_model text,p_calibration text)
RETURNS TABLE(dataset_sha text,runtime_lock_sha text,device_profile_sha text,issuer_id uuid,required_checks text[],test_fixture_shas text[],calibration_shas text[],evidence_shas text[],report_id uuid,report_sha text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p.dataset_sha,p.runtime_lock_sha,p.device_profile_sha,p.issuer_id,p.required_checks,p.test_fixture_shas,p.calibration_shas,p.evidence_shas,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes
 FROM photo.v2_qualification_policies p JOIN photo.v2_workflows w ON w.tenant_id=p_tenant AND w.run_id=p_run
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=w.project_id
 AND q.operation=p.operation AND q.operation_version=p.operation_version AND q.model_lock_sha=p.model_lock_sha
 AND q.runtime_lock_sha=p.runtime_lock_sha AND q.backend=p.backend AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 JOIN photo.artifacts a ON a.tenant_id=q.tenant_id AND a.id=q.report_artifact_id AND a.sha256=q.report_sha
 WHERE photo.v27_semantic_assembly_lease_live(p_tenant,p_run,p_expansion,p_token)
 AND EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_inputs(p_tenant,p_run))
 AND p.operation='semantic.transfer' AND p.operation_version='1' AND p.backend='cuda'
 AND p.model_lock_sha=p_model AND p_calibration=ANY(p.calibration_shas)
 AND p.enabled AND p.admission_enabled AND p.license_cleared AND q.trusted_import AND q.status='passed'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=p.id::text)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 -- Deterministic report choice; verify its actual bytes against current policy.
 ORDER BY q.report_sha,q.id LIMIT 1
$$;
ALTER FUNCTION photo.v27_semantic_assembly_qualification(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_qualification(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_qualification(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
