-- A qualified report cannot authorize a different Apply worker profile.
-- Existing unpinned series.balance tasks remain usable in Apply, but cannot
-- become training holdouts until their producer and attempt locks are exact.
-- +goose Up
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 RENAME TO v23_training_frozen_complete_pre_series_profile_core;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete_pre_series_profile_core(
 uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_complete(
 p_tenant uuid,p_run uuid,p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; event_id uuid; project_id uuid; actor_id uuid;
 source_row record; has_series boolean:=false;
BEGIN
 IF NOT photo.v23_training_frozen_complete_pre_series_profile_core(
  p_tenant,p_run,p_manifest) THEN RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF item->'render_case' ? 'series_evidence' THEN has_series:=true; EXIT; END IF;
 END LOOP;
 IF NOT has_series THEN RETURN true; END IF;
 SELECT p.project_id,r.created_by INTO project_id,actor_id FROM photo.runs r
 JOIN photo.learning_profiles p ON p.tenant_id=r.tenant_id AND
  p.id=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND
  NOT r.cancel_requested;
 IF project_id IS NULL OR actor_id IS NULL THEN RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF NOT (item->'render_case' ? 'series_evidence') THEN CONTINUE; END IF;
  event_id:=(item->>'event_id')::uuid;
  SELECT * INTO source_row FROM photo.v23_series_event_source_evaluate_core(
   p_tenant,project_id,actor_id,NULL::uuid,event_id);
  IF NOT FOUND THEN RETURN false; END IF;
  IF NOT EXISTS(
   SELECT 1 FROM photo.tasks producer
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND
    attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND
    attempt.fencing_token=producer.fencing_token AND
    attempt.finished_at IS NOT NULL
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND
    result.id=producer.canonical_result_id AND result.task_id=producer.id AND
    result.attempt=attempt.attempt AND result.fencing_token=attempt.fencing_token
   JOIN photo.v2_qualification_policies policy ON
    policy.operation='series.balance' AND policy.operation_version='1' AND
    policy.backend='cpu' AND policy.enabled AND policy.admission_enabled AND
    policy.license_cleared AND policy.required_checks @>
     ARRAY['v23_series_holdout_replay','v23_series_holdout_quality']::text[]
   JOIN photo.model_qualifications qualified ON
    qualified.tenant_id=p_tenant AND qualified.project_id=project_id AND
    qualified.operation=policy.operation AND
    qualified.operation_version=policy.operation_version AND
    qualified.model_lock_sha=policy.model_lock_sha AND
    qualified.runtime_lock_sha=policy.runtime_lock_sha AND
    qualified.backend=policy.backend AND
    qualified.device_profile_sha=policy.device_profile_sha AND
    qualified.dataset_sha=policy.dataset_sha AND
    qualified.trusted_import AND qualified.status='passed'
   JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id AND
    report.id=qualified.report_artifact_id AND
    report.sha256=qualified.report_sha AND report.state='ready' AND
    report.media_type='application/json' AND report.verified_at IS NOT NULL AND
    report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND
    (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
   WHERE producer.tenant_id=p_tenant AND
    producer.run_id=source_row.apply_run_id AND
    producer.id=source_row.producer_task_id AND
    producer.operation='series.balance' AND producer.operation_version='1' AND
    producer.state='succeeded' AND producer.resource_class='cpu' AND
    producer.execution_profile->>'backend'='cpu' AND attempt.backend='cpu' AND
    attempt.profile->>'backend'='cpu' AND
    producer.execution_profile->>'model_lock_sha'=policy.model_lock_sha AND
    attempt.profile->>'model_lock_sha'=policy.model_lock_sha AND
    producer.execution_profile->>'runtime_lock_sha'=policy.runtime_lock_sha AND
    attempt.profile->>'runtime_lock_sha'=policy.runtime_lock_sha AND
    producer.execution_profile->>'device_profile_sha'=policy.device_profile_sha AND
    attempt.profile->>'device_profile_sha'=policy.device_profile_sha
  ) THEN RETURN false; END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
