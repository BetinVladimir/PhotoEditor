-- +goose Up
-- No synthetic qualification: only trusted imported reports under DBA policy.
-- Locks remain held until the caller admission transaction commits.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_policy_sha(p_policy uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p photo.v2_qualification_policies%ROWTYPE; checks jsonb; fixtures jsonb; calibrations jsonb; evidence jsonb; encoded text; values_array text[];
BEGIN
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=p_policy;
 IF NOT FOUND OR p.operation NOT IN('portrait.hair','portrait.clothing') OR p.operation_version<>'1' OR p.backend<>'cpu' OR NOT p.license_cleared
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
ALTER FUNCTION photo.v28_local_policy_sha(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_policy_sha(uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_runtime_qualification_ready(p_tenant uuid,p_project uuid,p_operation text,p_runtime_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;selected_policy uuid;found_report boolean:=false;
BEGIN
 IF p_operation IS NULL OR p_operation NOT IN('portrait.hair','portrait.clothing')
  OR coalesce(p_runtime_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 FOR candidate IN
  SELECT policy.id AS policy_id FROM photo.v2_qualification_policies policy
  JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=p_project
   AND q.operation=policy.operation AND q.operation_version=policy.operation_version AND q.backend=policy.backend
   AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
   AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
  JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
  WHERE policy.operation=p_operation AND policy.operation_version='1' AND policy.backend='cpu'
   AND photo.v28_local_policy_sha(policy.id) IS NOT NULL AND policy.runtime_lock_sha=p_runtime_sha AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
   AND q.trusted_import AND q.status='passed'
   AND report.state='ready' AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
   AND report.verified_at IS NOT NULL AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
   AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
    AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
  ORDER BY policy.id,q.id,report.id FOR SHARE OF policy,q,report
 LOOP
  IF selected_policy IS NOT NULL AND selected_policy<>candidate.policy_id THEN RETURN false;END IF;
  selected_policy:=candidate.policy_id;found_report:=true;
 END LOOP;
 RETURN found_report;
END $$;
ALTER FUNCTION photo.v28_local_runtime_qualification_ready(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_runtime_qualification_ready(uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_local_runtime_qualification_ready(uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
