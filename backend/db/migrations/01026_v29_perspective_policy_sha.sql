-- +goose Up
-- Canonical private perspective CPU policy helper; does not admit any task.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_policy_sha(p_policy uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p photo.v2_qualification_policies%ROWTYPE; checks jsonb; fixtures jsonb; calibrations jsonb; evidence jsonb; encoded text; values_array text[];
BEGIN
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=p_policy;
 IF NOT FOUND OR p.operation<>'optical.perspective' OR p.operation_version<>'1' OR p.backend<>'cpu' OR NOT p.license_cleared
 OR p.required_checks IS NULL OR cardinality(p.required_checks) NOT BETWEEN 1 AND 1000 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p.required_checks) x WHERE x IS NULL OR octet_length(x) NOT BETWEEN 1 AND 100)
 OR cardinality(p.required_checks)<>(SELECT count(DISTINCT x) FROM unnest(p.required_checks) x) THEN RETURN NULL;END IF;
 IF p.test_fixture_shas IS NULL OR p.evidence_shas IS NULL OR p.calibration_shas IS NULL OR cardinality(p.calibration_shas)<1 OR cardinality(p.test_fixture_shas)<1 OR cardinality(p.evidence_shas)<1 THEN RETURN NULL;END IF;
 FOR values_array IN SELECT p.test_fixture_shas UNION ALL SELECT p.calibration_shas UNION ALL SELECT p.evidence_shas LOOP
 IF values_array IS NULL OR cardinality(values_array) NOT BETWEEN 0 AND 10000 OR EXISTS(SELECT 1 FROM unnest(values_array) x WHERE x IS NULL OR x !~ '^[0-9a-f]{64}$')
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
ALTER FUNCTION photo.v29_perspective_policy_sha(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_policy_sha(uuid) FROM PUBLIC;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
