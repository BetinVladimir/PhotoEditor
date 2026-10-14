-- Series holdouts become eligible only after a DBA-selected CPU qualification
-- explicitly covers both replay fidelity and held-out quality. With no such
-- policy/report, the 02093 refusal remains the observable behavior.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_holdout_qualified(
 p_tenant uuid,p_project uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT NULL AND p_project IS NOT NULL AND EXISTS(
  SELECT 1 FROM photo.v2_qualification_policies policy
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
  WHERE policy.operation='series.balance' AND policy.operation_version='1' AND
   policy.backend='cpu' AND policy.enabled AND policy.admission_enabled AND
   policy.license_cleared AND
   policy.required_checks @> ARRAY['v23_series_holdout_replay',
    'v23_series_holdout_quality']::text[] AND
   EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') AND
   EXISTS(SELECT 1 FROM photo.projects project WHERE
    project.tenant_id=p_tenant AND project.id=p_project)
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_holdout_qualified(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_holdout_qualified(uuid,uuid)
 FROM PUBLIC,photo_scheduler;

ALTER FUNCTION photo.v23_training_frozen_complete(uuid,uuid,jsonb)
 RENAME TO v23_training_frozen_complete_series_deny_core;
REVOKE ALL ON FUNCTION photo.v23_training_frozen_complete_series_deny_core(
 uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_frozen_complete(
 p_tenant uuid,p_run uuid,p_manifest jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; has_series boolean:=false; project_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR
  jsonb_typeof(p_manifest->'examples') IS DISTINCT FROM 'array' THEN
  RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_manifest->'examples') LOOP
  IF item->'render_case' ? 'series_evidence' THEN
   has_series:=true;
   EXIT;
  END IF;
 END LOOP;
 IF NOT has_series THEN
  RETURN photo.v23_training_frozen_complete_series_deny_core(
   p_tenant,p_run,p_manifest);
 END IF;
 -- Preserve every prior frozen recipe/defect/dust check; only the explicit
 -- 02093 deny is replaced for the series path.
 IF NOT photo.v23_training_frozen_complete_pre_series_core(
  p_tenant,p_run,p_manifest) THEN RETURN false; END IF;
 SELECT p.project_id INTO project_id FROM photo.runs r
 JOIN photo.learning_profiles p ON p.tenant_id=r.tenant_id AND
  p.id=NULLIF(r.config_snapshot->'v23_training'->>'profile_id','')::uuid
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND
  NOT r.cancel_requested;
 IF project_id IS NULL OR NOT photo.v23_series_holdout_qualified(
  p_tenant,project_id) THEN RETURN false; END IF;
 RETURN photo.v23_series_frozen_ready(p_tenant,p_run,p_manifest);
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
