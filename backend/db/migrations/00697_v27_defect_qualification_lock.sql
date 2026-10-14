-- +goose Up
-- Hold selected defect policy, imported qualification and exact original report.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_defect_qualification(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_expected jsonb,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE qualified record; matches integer:=0;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 OR jsonb_typeof(p_pin) IS DISTINCT FROM 'object' OR pg_column_size(p_pin)>16384 THEN RETURN false; END IF;
 FOR qualified IN
 SELECT q.id,policy.id AS policy_id FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project
 AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
 AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.operation IN('defects.propose') AND policy.operation=p_expected->>'Operation' AND policy.operation_version='1'
 AND p_expected->>'Operation' IN('defects.propose') AND p_expected->>'Version'='1'
 AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
 AND policy.model_lock_sha=p_expected->>'ModelLockSHA' AND policy.runtime_lock_sha=p_expected->>'RuntimeLockSHA'
 AND policy.backend=p_expected->>'Backend' AND policy.device_profile_sha=p_expected->>'DeviceProfileSHA'
 AND policy.dataset_sha=p_expected->>'DatasetSHA' AND policy.issuer_id::text=p_expected->>'IssuerID'
 AND (p_expected->>'LicenseCleared')::boolean
 AND to_jsonb(ARRAY(SELECT unnest(policy.required_checks) ORDER BY 1))=(SELECT jsonb_agg(value ORDER BY value) FROM jsonb_array_elements_text(p_expected->'RequiredChecks'))
 AND q.trusted_import AND q.status='passed'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=p_tenant AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 AND jsonb_build_object('contract','QualificationReport','artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,'key',report.object_key,'version',report.object_version,'bytes',report.bytes,'backend',policy.backend)=p_pin
 FOR SHARE OF policy,q,report
 LOOP
 PERFORM log.id FROM photo.audit_log log WHERE log.tenant_id=p_tenant
 AND log.action='v2_qualification.imported' AND log.resource_type='model_qualification'
 AND log.resource_id=qualified.id AND log.details->>'policy_id'=qualified.policy_id::text
 ORDER BY log.id LIMIT 1 FOR SHARE;
 IF NOT FOUND THEN RETURN false; END IF;
 matches:=matches+1;
 END LOOP;
 RETURN matches=1;
END $$;
ALTER FUNCTION photo.lock_semantic_proposal_defect_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_defect_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_defect_qualification(uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
