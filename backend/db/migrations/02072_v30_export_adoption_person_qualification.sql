-- +goose Up
-- Operator-selected person qualification under the current export lease;
-- physical report bytes and semantic checks remain mandatory in Go.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_person_qualification_object(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_expected jsonb,p_report uuid,p_report_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_report IS NULL OR coalesce(p_report_sha,'') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object'
  OR p_expected->>'Operation' IS DISTINCT FROM 'mask.person'
  OR p_expected->>'Version' IS DISTINCT FROM '1'
  OR p_expected->>'LicenseCleared' IS DISTINCT FROM 'true'
 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY SELECT report.bucket,report.object_key,report.object_version,
  report.sha256,report.bytes,report.media_type
 FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant
  AND q.project_id=w.project_id AND q.operation=policy.operation
  AND q.operation_version=policy.operation_version
  AND q.model_lock_sha=policy.model_lock_sha
  AND q.runtime_lock_sha=policy.runtime_lock_sha
  AND q.backend=policy.backend
  AND q.device_profile_sha=policy.device_profile_sha
  AND q.dataset_sha=policy.dataset_sha
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id
  AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.operation='mask.person' AND policy.operation_version='1'
  AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
  AND policy.model_lock_sha=p_expected->>'ModelLockSHA'
  AND policy.runtime_lock_sha=p_expected->>'RuntimeLockSHA'
  AND policy.backend=p_expected->>'Backend'
  AND policy.device_profile_sha=p_expected->>'DeviceProfileSHA'
  AND policy.dataset_sha=p_expected->>'DatasetSHA'
  AND policy.issuer_id::text=p_expected->>'IssuerID'
  AND to_jsonb(ARRAY(SELECT unnest(policy.required_checks) ORDER BY 1))=
   (SELECT jsonb_agg(value ORDER BY value)
    FROM jsonb_array_elements_text(p_expected->'RequiredChecks'))
  AND q.trusted_import AND q.status='passed'
  AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=p_tenant
   AND log.action='v2_qualification.imported'
   AND log.resource_type='model_qualification' AND log.resource_id=q.id
   AND log.details->>'policy_id'=policy.id::text)
  AND report.id=p_report AND report.sha256=p_report_sha
  AND report.state='ready' AND report.verified_at IS NOT NULL
  AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
  AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
  AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 FOR SHARE OF q,report;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_person_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_person_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_person_qualification_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,jsonb,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
