-- +goose Up
-- Donor qualification is independent of hair/clothing and remains disabled
-- until operator provisioned policy and trusted report evidence exist.
-- Exact report/calibration bytes must be verified before calling this lock.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_donor_policy_sha(p_policy uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p photo.v2_qualification_policies%ROWTYPE; checks jsonb; fixtures jsonb; calibrations jsonb; evidence jsonb; encoded text; values_array text[];
BEGIN
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=p_policy;
 IF NOT FOUND OR p.operation<>'portrait.open_eyes' OR p.operation_version<>'1' OR p.backend<>'cpu' OR NOT p.license_cleared
 OR cardinality(p.required_checks) NOT BETWEEN 1 AND 1000 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p.required_checks) x WHERE x IS NULL OR octet_length(x) NOT BETWEEN 1 AND 100)
 OR cardinality(p.required_checks)<>(SELECT count(DISTINCT x) FROM unnest(p.required_checks) x) THEN RETURN NULL;END IF;
 IF cardinality(p.test_fixture_shas)<1 OR cardinality(p.evidence_shas)<1 THEN RETURN NULL;END IF;
 FOR values_array IN SELECT p.test_fixture_shas UNION ALL SELECT p.calibration_shas UNION ALL SELECT p.evidence_shas LOOP
 IF cardinality(values_array) NOT BETWEEN 0 AND 10000 OR EXISTS(SELECT 1 FROM unnest(values_array) x WHERE x IS NULL OR x !~ '^[0-9a-f]{64}$')
 OR cardinality(values_array)<>(SELECT count(DISTINCT x) FROM unnest(values_array) x) THEN RETURN NULL;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM unnest(p.test_fixture_shas) x WHERE x=ANY(p.calibration_shas)) THEN RETURN NULL;END IF;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO checks FROM unnest(p.required_checks) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO fixtures FROM unnest(p.test_fixture_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO calibrations FROM unnest(p.calibration_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO evidence FROM unnest(p.evidence_shas) x;
 encoded:=photo.v27_semantic_policy_json(jsonb_build_object('schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,
 'backend',p.backend,'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,'dataset_sha',p.dataset_sha,
 'issuer_id',p.issuer_id,'required_checks',checks,'test_fixture_shas',fixtures,'calibration_shas',coalesce(calibrations,'[]'::jsonb),'evidence_shas',evidence,'license_cleared',p.license_cleared));
 RETURN encode(sha256(convert_to(encoded,'UTF8')),'hex');
END $$;
ALTER FUNCTION photo.v28_donor_policy_sha(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_donor_policy_sha(uuid) FROM PUBLIC;

CREATE FUNCTION photo.lock_v28_donor_qualification(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb:=p_pin; field text; verified uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(pin) IS DISTINCT FROM 'object' OR pg_column_size(pin)>16384
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false;END IF;
 IF NOT(pin ?& ARRAY['policy_id','qualification_id','policy_sha','report','calibration','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend'])
 OR (pin-'policy_id'-'qualification_id'-'policy_sha'-'report'-'calibration'-'model_lock_sha'-'runtime_lock_sha'-'device_profile_sha'-'dataset_sha'-'issuer_id'-'backend')<>'{}'::jsonb
 OR jsonb_typeof(pin->'report') IS DISTINCT FROM 'object' OR jsonb_typeof(pin->'calibration') IS DISTINCT FROM 'object'
 OR NOT(pin->'report' ?& ARRAY['artifact_id','sha256']) OR NOT(pin->'calibration' ?& ARRAY['artifact_id','sha256'])
 OR ((pin->'report')-'artifact_id'-'sha256')<>'{}'::jsonb OR ((pin->'calibration')-'artifact_id'-'sha256')<>'{}'::jsonb
 OR pin->>'backend' IS DISTINCT FROM 'cpu' OR coalesce(pin->>'policy_sha','') !~ '^[0-9a-f]{64}$'
 OR pin->'report'->>'artifact_id'=pin->'calibration'->>'artifact_id' THEN RETURN false;END IF;
 FOR field IN SELECT unnest(ARRAY['policy_id','qualification_id','policy_sha','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id','backend']) LOOP
 IF jsonb_typeof(pin->field) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 FOR field IN SELECT unnest(ARRAY['report','calibration']) LOOP
 IF jsonb_typeof(pin->field->'artifact_id') IS DISTINCT FROM 'string'
 OR jsonb_typeof(pin->field->'sha256') IS DISTINCT FROM 'string' THEN RETURN false;END IF;
 END LOOP;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT q.id INTO verified FROM photo.v2_qualification_policies p
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project AND q.id=(pin->>'qualification_id')::uuid
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 JOIN photo.artifacts calibration ON calibration.tenant_id=p_tenant AND calibration.id=(pin->'calibration'->>'artifact_id')::uuid
 WHERE p.id=(pin->>'policy_id')::uuid AND p.operation='portrait.open_eyes' AND p.operation_version='1'
 AND p.backend='cpu' AND p.enabled AND p.admission_enabled AND p.license_cleared
 AND p.model_lock_sha=pin->>'model_lock_sha' AND p.runtime_lock_sha=pin->>'runtime_lock_sha'
 AND p.device_profile_sha=pin->>'device_profile_sha' AND p.dataset_sha=pin->>'dataset_sha' AND p.issuer_id=(pin->>'issuer_id')::uuid
 AND photo.v28_donor_policy_sha(p.id)=pin->>'policy_sha'
 AND q.operation=p.operation AND q.operation_version=p.operation_version AND q.backend=p.backend
 AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha AND q.device_profile_sha=p.device_profile_sha AND q.dataset_sha=p.dataset_sha
 AND q.trusted_import AND q.status='passed' AND report.id=(pin->'report'->>'artifact_id')::uuid AND report.sha256=pin->'report'->>'sha256'
 AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
 AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=p.id::text)
 AND report.state='ready' AND report.verified_at IS NOT NULL AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null') AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 AND calibration.sha256=pin->'calibration'->>'sha256' AND calibration.sha256=ANY(p.calibration_shas)
 AND calibration.state='ready' AND calibration.verified_at IS NOT NULL AND calibration.media_type='application/json' AND calibration.bytes BETWEEN 2 AND 1048576
 AND calibration.object_version IS NOT NULL AND calibration.object_version NOT IN('','null') AND (calibration.expires_at IS NULL OR calibration.expires_at>clock_timestamp()) FOR SHARE OF p,q,report,calibration;
 RETURN verified IS NOT NULL;

EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;

ALTER FUNCTION photo.lock_v28_donor_qualification(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_donor_qualification(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_donor_qualification(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
