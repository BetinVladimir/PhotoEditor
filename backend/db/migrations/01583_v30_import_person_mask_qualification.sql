-- +goose Up
-- Scheduler observation only; final execution admission must qualify again.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_person_mask_qualification_object(p_tenant uuid,p_workflow uuid,p_body text,p_expected jsonb,p_report uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sources jsonb;project uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_report IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR p_expected->>'Operation' IS DISTINCT FROM 'mask.person' OR p_expected->>'Version' IS DISTINCT FROM '1'
 OR p_expected->>'LicenseCleared' IS DISTINCT FROM 'true' THEN RETURN;END IF;
 sources:=photo.v30_interchange_import_sources(p_tenant,p_workflow,p_body);
 IF sources IS NULL THEN RETURN;END IF;
 project:=(sources->>'project_id')::uuid;
 RETURN QUERY SELECT report.id,report.sha256,report.bucket,report.object_key,report.object_version,report.bytes
 FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=project
 AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
 AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.operation='mask.person' AND policy.operation_version='1'
 AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
 AND policy.backend=p_expected->>'Backend' AND policy.backend IN('cpu','cuda','mps')
 AND policy.model_lock_sha=p_expected->>'ModelLockSHA' AND policy.runtime_lock_sha=p_expected->>'RuntimeLockSHA'
 AND policy.device_profile_sha=p_expected->>'DeviceProfileSHA' AND policy.dataset_sha=p_expected->>'DatasetSHA'
 AND policy.issuer_id::text=p_expected->>'IssuerID'
 AND to_jsonb(ARRAY(SELECT unnest(policy.required_checks) ORDER BY 1))=(SELECT coalesce(jsonb_agg(value ORDER BY value),'[]'::jsonb) FROM jsonb_array_elements_text(p_expected->'RequiredChecks'))
 AND to_jsonb(ARRAY(SELECT unnest(policy.test_fixture_shas) ORDER BY 1))=(SELECT coalesce(jsonb_agg(key ORDER BY key),'[]'::jsonb) FROM jsonb_object_keys(p_expected->'TestFixtureSHAs') key)
 AND to_jsonb(ARRAY(SELECT unnest(policy.calibration_shas) ORDER BY 1))=(SELECT coalesce(jsonb_agg(key ORDER BY key),'[]'::jsonb) FROM jsonb_object_keys(p_expected->'CalibrationSHAs') key)
 AND to_jsonb(ARRAY(SELECT unnest(policy.evidence_shas) ORDER BY 1))=(SELECT coalesce(jsonb_agg(key ORDER BY key),'[]'::jsonb) FROM jsonb_object_keys(p_expected->'EvidenceSHAs') key)
 AND q.trusted_import AND q.status='passed'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=p_tenant AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
 AND report.id=p_report AND report.sha256=p_sha AND report.state='ready' AND report.verified_at IS NOT NULL
 AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 ORDER BY policy.id,q.id FOR SHARE OF policy,q,report;
END $$;
ALTER FUNCTION photo.v30_import_person_mask_qualification_object(uuid,uuid,text,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_person_mask_qualification_object(uuid,uuid,text,jsonb,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_person_mask_qualification_object(uuid,uuid,text,jsonb,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
