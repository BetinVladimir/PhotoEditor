-- +goose Up
-- Scheduler-side current catalog lock before a restoration pin is placed in a
-- draft execution profile. Worker admission independently rechecks the catalog.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_restoration_qualification(p_tenant uuid,p_project uuid,
 p_actor uuid,p_session uuid,p_operation text,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb:=p_pin;matched uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_operation NOT IN ('portrait.glare','image.reflection')
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
  OR jsonb_typeof(pin) IS DISTINCT FROM 'object' OR pg_column_size(pin)>16384
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(pin) k) IS DISTINCT FROM
    ARRAY['backend','calibration','dataset_sha','device_profile_sha','issuer_id',
          'model_lock_sha','operation','operation_version','policy_id','policy_sha',
          'qualification_id','report','runtime_lock_sha']::text[]
  OR pin->>'operation' IS DISTINCT FROM p_operation
  OR pin->>'operation_version' IS DISTINCT FROM '1'
  OR (pin->>'backend' IN ('cpu','cuda')) IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(pin->'report') IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(pin->'calibration') IS DISTINCT FROM true
  OR pin->'report'->>'artifact_id' IS NOT DISTINCT FROM pin->'calibration'->>'artifact_id'
  OR coalesce(pin->>'policy_sha','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project
  AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT p.id INTO matched FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant
  AND q.project_id=p_project AND q.id=(pin->>'qualification_id')::uuid
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id
  AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant
  AND calibration.id=(pin->'calibration'->>'artifact_id')::uuid
 WHERE p.id=(pin->>'policy_id')::uuid AND p.operation=p_operation
  AND p.operation_version='1' AND p.backend=pin->>'backend'
  AND p.enabled AND p.admission_enabled AND p.license_cleared
  AND p.model_lock_sha=pin->>'model_lock_sha'
  AND p.runtime_lock_sha=pin->>'runtime_lock_sha'
  AND p.device_profile_sha=pin->>'device_profile_sha'
  AND p.dataset_sha=pin->>'dataset_sha'
  AND p.issuer_id::text=pin->>'issuer_id'
  AND photo.v28_restoration_policy_sha(p.id)=pin->>'policy_sha'
  AND q.operation=p.operation AND q.operation_version=p.operation_version
  AND q.backend=p.backend AND q.model_lock_sha=p.model_lock_sha
  AND q.runtime_lock_sha=p.runtime_lock_sha
  AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
  AND q.trusted_import AND q.status='passed'
  AND report.id::text=pin->'report'->>'artifact_id'
  AND report.sha256=pin->'report'->>'sha256'
  AND report.state='ready' AND report.verified_at IS NOT NULL
  AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
  AND report.object_version IS NOT NULL AND report.object_version NOT IN ('','null')
  AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
  AND calibration.sha256=pin->'calibration'->>'sha256'
  AND calibration.sha256=ANY(p.calibration_shas)
  AND calibration.state='ready' AND calibration.verified_at IS NOT NULL
  AND calibration.media_type='application/json'
  AND calibration.bytes BETWEEN 2 AND 1048576
  AND calibration.object_version IS NOT NULL
  AND calibration.object_version NOT IN ('','null')
  AND (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id
   AND log.action='v2_qualification.imported'
   AND log.resource_type='model_qualification' AND log.resource_id=q.id
   AND log.details->>'policy_id'=p.id::text)
 FOR SHARE OF p,q,report,calibration;
 RETURN matched IS NOT NULL;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_v28_restoration_qualification(uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_restoration_qualification(uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_restoration_qualification(uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
