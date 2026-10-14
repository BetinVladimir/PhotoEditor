-- +goose Up
-- Face detector export qualification uses its own operator-provisioned admission policy.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_face_detector_qualification_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected jsonb)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT report.id,report.sha256,report.bucket,report.object_key,report.object_version,report.bytes
 FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project
 AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
 AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND policy.operation='face.detect'
 AND policy.operation=p_expected->>'Operation' AND policy.operation_version='1'
 AND p_expected->>'Operation'='face.detect' AND p_expected->>'Version'='1'
 AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
 AND policy.model_lock_sha=p_expected->>'ModelLockSHA' AND policy.runtime_lock_sha=p_expected->>'RuntimeLockSHA'
 AND policy.backend IN('cpu','cuda','mps') AND p_expected->>'Backend' IN('cpu','cuda','mps') AND policy.backend=p_expected->>'Backend' AND policy.device_profile_sha=p_expected->>'DeviceProfileSHA'
 AND policy.dataset_sha=p_expected->>'DatasetSHA' AND policy.issuer_id::text=p_expected->>'IssuerID'
 AND (p_expected->>'LicenseCleared')::boolean
 AND to_jsonb(ARRAY(SELECT unnest(policy.required_checks) ORDER BY 1))=(SELECT jsonb_agg(value ORDER BY value) FROM jsonb_array_elements_text(p_expected->'RequiredChecks'))
 AND q.trusted_import AND q.status='passed'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=p_tenant AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 ORDER BY report.id;
$$;
ALTER FUNCTION photo.v30_face_detector_qualification_object(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_face_detector_qualification_object(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_face_detector_qualification_object(uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
