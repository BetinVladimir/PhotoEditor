-- +goose Up
-- Private exact downstream qualification proof. Actual report bytes must also be validated by the caller.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_downstream_qualification_pin_ready(p_tenant uuid,p_project uuid,p_operation text,p_version text,p_backend text,pin jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE field text;
BEGIN
 IF p_operation IS NULL OR p_version IS NULL OR p_backend NOT IN('cpu','cuda','mps') OR p_backend IS NULL
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL)
 OR jsonb_typeof(pin) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 IF NOT(pin ?& ARRAY['policy_id','qualification_id','policy_sha','report','calibration','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend'])
 OR (pin-'policy_id'-'qualification_id'-'policy_sha'-'report'-'calibration'-'model_lock_sha'-'runtime_lock_sha'-'device_profile_sha'-'dataset_sha'-'issuer_id'-'backend')<>'{}'::jsonb
 OR jsonb_typeof(pin->'report') IS DISTINCT FROM 'object' OR jsonb_typeof(pin->'calibration') IS DISTINCT FROM 'object'
 OR NOT(pin->'report' ?& ARRAY['artifact_id','sha256']) OR NOT(pin->'calibration' ?& ARRAY['artifact_id','sha256'])
 OR ((pin->'report')-'artifact_id'-'sha256')<>'{}'::jsonb OR ((pin->'calibration')-'artifact_id'-'sha256')<>'{}'::jsonb
 OR pin->>'backend' IS DISTINCT FROM p_backend OR coalesce(pin->>'policy_sha','') !~ '^[0-9a-f]{64}$'
 OR pin->'report'->>'artifact_id'=pin->'calibration'->>'artifact_id' THEN RETURN false;END IF;
 FOR field IN SELECT unnest(ARRAY['policy_id','qualification_id','policy_sha','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend']) LOOP
 IF jsonb_typeof(pin->field) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 FOR field IN SELECT unnest(ARRAY['report','calibration']) LOOP
 IF jsonb_typeof(pin->field->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(pin->field->'sha256') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 PERFORM id FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND project_id=p_project AND id=(pin->>'qualification_id')::uuid FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN((pin->'report'->>'artifact_id')::uuid,(pin->'calibration'->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 RETURN EXISTS(SELECT 1 FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project AND q.id=(pin->>'qualification_id')::uuid
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant AND calibration.id=(pin->'calibration'->>'artifact_id')::uuid
 WHERE p.id=(pin->>'policy_id')::uuid AND p.operation=p_operation AND p.operation_version=p_version
 AND p.backend=p_backend AND p.enabled AND p.admission_enabled AND p.license_cleared
 AND p.model_lock_sha=pin->>'model_lock_sha' AND p.runtime_lock_sha=pin->>'runtime_lock_sha'
 AND p.device_profile_sha=pin->>'device_profile_sha' AND p.dataset_sha=pin->>'dataset_sha' AND p.issuer_id=(pin->>'issuer_id')::uuid
 AND photo.v27_semantic_policy_sha(p.id)=pin->>'policy_sha'
 AND q.operation=p.operation AND q.operation_version=p.operation_version AND q.backend=p.backend
 AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 AND q.trusted_import AND q.status='passed' AND report.id=(pin->'report'->>'artifact_id')::uuid AND report.sha256=pin->'report'->>'sha256'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=p.id::text)
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 AND calibration.sha256=pin->'calibration'->>'sha256' AND calibration.sha256=ANY(p.calibration_shas)
 AND calibration.state='ready' AND calibration.verified_at IS NOT NULL AND calibration.media_type='application/json' AND calibration.bytes BETWEEN 2 AND 1048576
 AND calibration.object_version IS NOT NULL AND calibration.object_version NOT IN('','null') AND (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_downstream_qualification_pin_ready(uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_downstream_qualification_pin_ready(uuid,uuid,text,text,text,jsonb) FROM PUBLIC;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
