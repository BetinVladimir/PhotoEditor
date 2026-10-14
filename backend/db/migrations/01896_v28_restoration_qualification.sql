-- +goose Up
-- Dedicated policy identity for glare/reflection; donor open-eyes policy is
-- intentionally not accepted for either restoration operation.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_policy_sha(p_policy uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p photo.v2_qualification_policies%ROWTYPE; checks jsonb; fixtures jsonb;
 calibrations jsonb; evidence jsonb; encoded text; items text[];
BEGIN
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=p_policy;
 IF NOT FOUND OR p.operation NOT IN ('portrait.glare','image.reflection')
  OR p.operation_version<>'1' OR p.backend NOT IN ('cpu','cuda')
  OR NOT p.license_cleared OR cardinality(p.required_checks) NOT BETWEEN 1 AND 1000
  OR cardinality(p.test_fixture_shas)<1 OR cardinality(p.evidence_shas)<1
  THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p.required_checks) x
  WHERE x IS NULL OR octet_length(x) NOT BETWEEN 1 AND 100)
  OR cardinality(p.required_checks)<>
     (SELECT count(DISTINCT x) FROM unnest(p.required_checks) x) THEN RETURN NULL;END IF;
 FOR items IN SELECT p.test_fixture_shas UNION ALL SELECT p.calibration_shas
  UNION ALL SELECT p.evidence_shas LOOP
  IF cardinality(items) NOT BETWEEN 0 AND 10000
   OR EXISTS(SELECT 1 FROM unnest(items) x WHERE x IS NULL OR x !~ '^[0-9a-f]{64}$')
   OR cardinality(items)<>(SELECT count(DISTINCT x) FROM unnest(items) x)
   THEN RETURN NULL;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM unnest(p.test_fixture_shas) x
  WHERE x=ANY(p.calibration_shas)) THEN RETURN NULL;END IF;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO checks FROM unnest(p.required_checks) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO fixtures FROM unnest(p.test_fixture_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO calibrations FROM unnest(p.calibration_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO evidence FROM unnest(p.evidence_shas) x;
 encoded:=photo.v27_semantic_policy_json(jsonb_build_object(
  'schema_version',1,'policy_id',p.id,'operation',p.operation,
  'operation_version',p.operation_version,'backend',p.backend,
  'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,
  'device_profile_sha',p.device_profile_sha,'dataset_sha',p.dataset_sha,
  'issuer_id',p.issuer_id,'required_checks',checks,'test_fixture_shas',fixtures,
  'calibration_shas',coalesce(calibrations,'[]'::jsonb),'evidence_shas',evidence,
  'license_cleared',p.license_cleared));
 RETURN encode(sha256(convert_to(encoded,'UTF8')),'hex');
END $$;
ALTER FUNCTION photo.v28_restoration_policy_sha(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_policy_sha(uuid) FROM PUBLIC;

CREATE FUNCTION photo.v28_restoration_qualification_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb;project_id uuid;t photo.tasks%ROWTYPE;policy_id uuid;body jsonb;
BEGIN
 IF photo.v28_restoration_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,
  p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 body:=p_json::jsonb;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=p_task FOR SHARE;
 SELECT project_id INTO project_id FROM photo.v2_workflows
  WHERE tenant_id=p_tenant AND run_id=p_run;
 pin:=t.execution_profile->'restoration_qualification';
 IF project_id IS NULL OR jsonb_typeof(pin) IS DISTINCT FROM 'object'
  OR pg_column_size(pin)>16384
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(pin) k) IS DISTINCT FROM
    ARRAY['backend','calibration','dataset_sha','device_profile_sha','issuer_id',
          'model_lock_sha','operation','operation_version','policy_id','policy_sha',
          'qualification_id','report','runtime_lock_sha']::text[]
  OR pin->>'operation' IS DISTINCT FROM t.operation
  OR pin->>'operation_version' IS DISTINCT FROM '1'
  OR pin->>'backend' IS DISTINCT FROM t.execution_profile->>'backend'
  OR pin->>'model_lock_sha' IS DISTINCT FROM body->>'model_lock_sha'
  OR pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'restoration_runtime_sha'
  OR pin->'calibration' IS DISTINCT FROM body->'calibration'
  OR photo.v25_layer_ref_shape(pin->'report') IS DISTINCT FROM true
  OR photo.v25_layer_ref_shape(pin->'calibration') IS DISTINCT FROM true
  OR coalesce(pin->>'policy_sha','') !~ '^[0-9a-f]{64}$'
  THEN RETURN false;END IF;
 SELECT p.id INTO policy_id FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant
  AND q.project_id=project_id AND q.id=(pin->>'qualification_id')::uuid
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id
  AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant
  AND calibration.id=(pin->'calibration'->>'artifact_id')::uuid
 WHERE p.id=(pin->>'policy_id')::uuid AND p.operation=t.operation
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
 IF policy_id IS NULL THEN RETURN false;END IF;
 RETURN photo.v28_restoration_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,
  p_worker,p_params,p_sha,p_json,p_digest);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_qualification_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_qualification_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_qualification_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
