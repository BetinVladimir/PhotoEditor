-- +goose Up
-- Recheck the exact DBA-selected qualification and the worker attempt inside
-- the identity adoption transaction. Dataset SHA is returned for calibration
-- verification; no caller-supplied model profile grants authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_live_qualification(p_tenant uuid,p_task uuid,
 p_result uuid,p_params uuid,p_params_sha text,p_model text,
 p_calibration uuid,p_calibration_sha text) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE;project uuid;dataset text;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
  nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_task IS NULL OR p_result IS NULL OR p_params IS NULL OR
  p_calibration IS NULL OR p_params_sha !~ '^[0-9a-f]{64}$' OR
  p_model !~ '^[0-9a-f]{64}$' OR
  p_calibration_sha !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO task_row FROM photo.tasks t WHERE t.tenant_id=p_tenant AND
  t.id=p_task AND t.operation='identity.compare' AND
  t.operation_version='1' AND t.resource_class='gpu' AND
  t.state='succeeded' AND NOT t.cancel_requested AND
  t.canonical_result_id=p_result AND t.params_artifact_id=p_params AND
  t.attempt>0 AND t.fencing_token>0 FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w
  JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=p_tenant AND w.run_id=task_row.run_id AND
   p.archived_at IS NULL AND NOT r.cancel_requested;
 IF project IS NULL OR NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE
  z.tenant_id=p_tenant AND z.id=p_result AND z.task_id=p_task AND
  z.attempt=task_row.attempt AND z.fencing_token=task_row.fencing_token) OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task AND
   i.contract='Params' AND i.artifact_id=p_params AND i.sha256=p_params_sha) OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task AND
   i.contract='Calibration' AND i.artifact_id=p_calibration AND
   i.sha256=p_calibration_sha) THEN RETURN NULL;END IF;
 SELECT policy.dataset_sha INTO dataset FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications qualified ON
  qualified.id=(task_row.execution_profile->>'qualification_id')::uuid AND
  qualified.tenant_id=p_tenant AND qualified.project_id=project AND
  qualified.operation=policy.operation AND
  qualified.operation_version=policy.operation_version AND
  qualified.model_lock_sha=policy.model_lock_sha AND
  qualified.runtime_lock_sha=policy.runtime_lock_sha AND
  qualified.backend=policy.backend AND
  qualified.device_profile_sha=policy.device_profile_sha AND
  qualified.dataset_sha=policy.dataset_sha AND
  qualified.trusted_import AND qualified.status='passed'
 JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id AND
  report.id=qualified.report_artifact_id AND report.sha256=qualified.report_sha AND
  report.state='ready' AND report.media_type='application/json' AND
  report.bytes BETWEEN 2 AND 8388608 AND report.verified_at IS NOT NULL AND
  report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND
  (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant AND
  calibration.id=p_calibration AND calibration.sha256=p_calibration_sha AND
  calibration.state='ready' AND calibration.media_type='application/json' AND
  calibration.bytes BETWEEN 2 AND 1048576 AND calibration.verified_at IS NOT NULL AND
  calibration.object_version IS NOT NULL AND calibration.object_version NOT IN('','null') AND
  (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp())
 JOIN photo.task_attempts attempt ON attempt.tenant_id=p_tenant AND
  attempt.task_id=p_task AND attempt.attempt=task_row.attempt AND
  attempt.fencing_token=task_row.fencing_token AND attempt.finished_at IS NOT NULL AND
  attempt.error IS NULL AND attempt.backend=policy.backend AND
  attempt.profile->>'backend'=policy.backend AND
  attempt.profile->>'model_lock_sha'=policy.model_lock_sha AND
  attempt.profile->>'runtime_lock_sha'=policy.runtime_lock_sha AND
  attempt.profile->>'device_profile_sha'=policy.device_profile_sha AND
  attempt.profile->>'dataset_sha'=policy.dataset_sha AND
  attempt.profile->>'qualification_policy_id'=policy.id::text AND
  attempt.profile->>'qualification_id'=qualified.id::text
 WHERE policy.id=(task_row.execution_profile->>'qualification_policy_id')::uuid AND
  policy.operation='identity.compare' AND policy.operation_version='1' AND
  policy.backend IN('cpu','cuda') AND policy.enabled AND
  policy.admission_enabled AND policy.license_cleared AND
  p_calibration_sha=ANY(policy.calibration_shas) AND
  task_row.execution_profile->>'backend'=policy.backend AND
  task_row.execution_profile->>'model_lock_sha'=policy.model_lock_sha AND
  task_row.execution_profile->>'runtime_lock_sha'=policy.runtime_lock_sha AND
  task_row.execution_profile->>'device_profile_sha'=policy.device_profile_sha AND
  task_row.execution_profile->>'dataset_sha'=policy.dataset_sha AND
  task_row.execution_profile->'qualification_report'=jsonb_build_object(
   'artifact_id',report.id,'sha256',report.sha256) AND
  task_row.execution_profile->'calibration'=jsonb_build_object(
   'artifact_id',p_calibration,'sha256',p_calibration_sha) AND
  task_row.execution_profile->>'model_lock_sha'=p_model
 FOR SHARE OF policy,qualified,report,calibration,attempt;
 RETURN dataset;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v23_identity_live_qualification(uuid,uuid,uuid,uuid,text,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_live_qualification(uuid,uuid,uuid,uuid,text,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_live_qualification(uuid,uuid,uuid,uuid,text,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
