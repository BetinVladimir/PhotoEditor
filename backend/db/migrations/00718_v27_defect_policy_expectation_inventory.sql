-- +goose Up
-- Full expectation equality against the frozen global policy inventory.
-- Composed gate already locks and re-discovers that inventory. No queue authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_policy_expectations_match(p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE inventory jsonb;policy jsonb;selected jsonb:='{}';op text;rows integer;wanted jsonb;entries jsonb;entry jsonb;family integer;policy_values jsonb;key text;shas jsonb;
BEGIN
 inventory:=p_evidence->'source_authority'->'policy';
 IF jsonb_typeof(inventory) IS DISTINCT FROM 'array' OR jsonb_array_length(inventory) NOT BETWEEN 1 AND 64 OR octet_length(inventory::text)>4194304 THEN RETURN false;END IF;
 FOREACH op IN ARRAY ARRAY['defects.propose','face.landmarks','face.detect','face.parse','mask.person'] LOOP
  SELECT count(*) INTO rows FROM jsonb_array_elements(inventory) item WHERE item->>'operation'=op AND item->>'operation_version'='1' AND item->'admission_enabled'='true'::jsonb;
  IF rows<>1 THEN RETURN false;END IF;
  SELECT item INTO policy FROM jsonb_array_elements(inventory) item WHERE item->>'operation'=op AND item->>'operation_version'='1' AND item->'admission_enabled'='true'::jsonb;
  IF policy->'enabled' IS DISTINCT FROM 'true'::jsonb OR policy->'license_cleared' IS DISTINCT FROM 'true'::jsonb THEN RETURN false;END IF;
  wanted:=jsonb_build_object('Operation',op,'Version','1','Backend',policy->'backend','ModelLockSHA',policy->'model_lock_sha','RuntimeLockSHA',policy->'runtime_lock_sha','DeviceProfileSHA',policy->'device_profile_sha','DatasetSHA',policy->'dataset_sha','IssuerID',policy->'issuer_id','LicenseCleared',true);
  policy_values:=policy->'required_checks';
  IF jsonb_typeof(policy_values) IS DISTINCT FROM 'array' OR jsonb_array_length(policy_values) NOT BETWEEN 1 AND 256 THEN RETURN false;END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(policy_values) v WHERE jsonb_typeof(v)<>'string' OR length(v#>>'{}') NOT BETWEEN 1 AND 256) OR (SELECT count(DISTINCT v) FROM jsonb_array_elements(policy_values) v)<>jsonb_array_length(policy_values) THEN RETURN false;END IF;
  wanted:=wanted||jsonb_build_object('RequiredChecks',(SELECT jsonb_agg(v ORDER BY v) FROM jsonb_array_elements(policy_values) v));
  FOREACH key IN ARRAY ARRAY['test_fixture_shas','calibration_shas','evidence_shas'] LOOP
   policy_values:=policy->key;
   IF jsonb_typeof(policy_values) IS DISTINCT FROM 'array' OR jsonb_array_length(policy_values)>4096 OR (key<>'calibration_shas' AND jsonb_array_length(policy_values)=0) THEN RETURN false;END IF;
   IF EXISTS(SELECT 1 FROM jsonb_array_elements(policy_values) v WHERE jsonb_typeof(v)<>'string' OR (v#>>'{}') !~ '^[0-9a-f]{64}$') OR (SELECT count(DISTINCT v) FROM jsonb_array_elements(policy_values) v)<>jsonb_array_length(policy_values) THEN RETURN false;END IF;
   SELECT coalesce(jsonb_object_agg(v#>>'{}','{}'::jsonb),'{}'::jsonb) INTO shas FROM jsonb_array_elements(policy_values) v;
   wanted:=wanted||jsonb_build_object(CASE key WHEN 'test_fixture_shas' THEN 'TestFixtureSHAs' WHEN 'calibration_shas' THEN 'CalibrationSHAs' ELSE 'EvidenceSHAs' END,shas);
  END LOOP;
  selected:=selected||jsonb_build_object(op,wanted);
 END LOOP;
 IF p_evidence->'source_authority'->'expected' IS DISTINCT FROM selected->'defects.propose' OR p_evidence->'person'->'expected' IS DISTINCT FROM selected->'mask.person' THEN RETURN false;END IF;
 FOR family IN 0..1 LOOP
  entries:=CASE family WHEN 0 THEN p_evidence->'faces' ELSE p_evidence->'parser'->'parsers' END;
  IF jsonb_typeof(entries) IS DISTINCT FROM 'array' OR jsonb_array_length(entries)>64 THEN RETURN false;END IF;
  FOR entry IN SELECT value FROM jsonb_array_elements(entries) LOOP
   wanted:=jsonb_build_array(selected->CASE family WHEN 0 THEN 'face.landmarks' ELSE 'face.parse' END,selected->'face.detect');
   IF entry->'expected' IS DISTINCT FROM wanted OR entry->'policies' IS DISTINCT FROM wanted THEN RETURN false;END IF;
  END LOOP;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_policy_expectations_match(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_policy_expectations_match(jsonb) FROM PUBLIC;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_admission_evidence_pre_policy;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence_pre_policy(uuid,uuid,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_defect_policy_expectations_match(p_evidence) IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_admission_evidence_pre_policy(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
