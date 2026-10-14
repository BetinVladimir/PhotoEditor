-- API may pin a series.balance Apply task only from a selected trusted
-- qualification and a live CPU worker with the exact runtime bundle lock.
-- Absence returns no row; ordinary Apply remains available but such a result
-- cannot enter the later V23 learning holdout gate.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_apply_qualification(
 p_tenant uuid,p_project uuid)
RETURNS TABLE(model_lock_sha text,runtime_lock_sha text,
 device_profile_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT policy.model_lock_sha,policy.runtime_lock_sha,
  policy.device_profile_sha
 FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications qualified ON
  qualified.tenant_id=p_tenant AND qualified.project_id=p_project AND
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
  report.bytes BETWEEN 2 AND 8388608 AND
  (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 WHERE p_tenant IS NOT NULL AND p_project IS NOT NULL AND
  nullif(current_setting('app.tenant_id',true),'')::uuid=p_tenant AND
  policy.operation='series.balance' AND policy.operation_version='1' AND
  policy.backend='cpu' AND policy.enabled AND policy.admission_enabled AND
  policy.license_cleared AND
  policy.required_checks @> ARRAY['v23_series_holdout_replay',
   'v23_series_holdout_quality']::text[] AND
  EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') AND
  EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=p_tenant AND
   p.id=p_project AND p.archived_at IS NULL) AND
  EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE
   w.tenant_id=p_tenant AND w.enabled AND
   w.last_seen>clock_timestamp()-interval '30 seconds' AND
   w.resource_classes ? 'cpu' AND w.backends ? 'cpu' AND
   (w.qualified_operations->'series.balance') ? 'cpu' AND
   w.lock_id=policy.runtime_lock_sha);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_apply_qualification(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_apply_qualification(uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_series_apply_qualification(uuid,uuid)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
