-- +goose Up
-- Canonical serialization restricted to qualification policy shapes:
-- objects, arrays, strings, booleans and schema_version integer 1; no floats.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_policy_json(p jsonb) RETURNS text
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE out text;
BEGIN
 CASE jsonb_typeof(p)
 WHEN 'object' THEN SELECT '{'||coalesce(string_agg(to_jsonb(x.key)::text||':'||photo.v27_semantic_policy_json(x.value),',' ORDER BY x.key COLLATE "C"),'')||'}' INTO out FROM jsonb_each(p) x;
 WHEN 'array' THEN SELECT '['||coalesce(string_agg(photo.v27_semantic_policy_json(x.value),',' ORDER BY x.ordinality),'')||']' INTO out FROM jsonb_array_elements(p) WITH ORDINALITY x;
 WHEN 'string' THEN out:=p::text;
 WHEN 'boolean' THEN out:=p::text;
 WHEN 'number' THEN IF p<>'1'::jsonb THEN RETURN NULL;END IF;out:='1';
 ELSE RETURN NULL;
 END CASE;
 RETURN out;
END $$;
ALTER FUNCTION photo.v27_semantic_policy_json(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_policy_json(jsonb) FROM PUBLIC;
CREATE FUNCTION photo.v27_semantic_policy_sha(p_policy uuid) RETURNS text
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE p photo.v2_qualification_policies%ROWTYPE; checks jsonb; fixtures jsonb; calibrations jsonb; evidence jsonb; encoded text; values_array text[];
BEGIN
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=p_policy;
 IF NOT FOUND OR p.operation<>'semantic.transfer' OR p.operation_version<>'1' OR p.backend<>'cuda' OR NOT p.license_cleared
 OR cardinality(p.required_checks) NOT BETWEEN 1 AND 1000 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p.required_checks) x WHERE x IS NULL OR length(x) NOT BETWEEN 1 AND 100)
 OR cardinality(p.required_checks)<>(SELECT count(DISTINCT x) FROM unnest(p.required_checks) x) THEN RETURN NULL;END IF;
 FOR values_array IN SELECT p.test_fixture_shas UNION ALL SELECT p.calibration_shas UNION ALL SELECT p.evidence_shas LOOP
 IF cardinality(values_array) NOT BETWEEN 1 AND 10000 OR EXISTS(SELECT 1 FROM unnest(values_array) x WHERE x IS NULL OR x !~ '^[0-9a-f]{64}$')
 OR cardinality(values_array)<>(SELECT count(DISTINCT x) FROM unnest(values_array) x) THEN RETURN NULL;END IF;
 END LOOP;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO checks FROM unnest(p.required_checks) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO fixtures FROM unnest(p.test_fixture_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO calibrations FROM unnest(p.calibration_shas) x;
 SELECT jsonb_agg(x ORDER BY x COLLATE "C") INTO evidence FROM unnest(p.evidence_shas) x;
 encoded:=photo.v27_semantic_policy_json(jsonb_build_object('schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,
 'backend',p.backend,'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,'dataset_sha',p.dataset_sha,
 'issuer_id',p.issuer_id,'required_checks',checks,'test_fixture_shas',fixtures,'calibration_shas',calibrations,'evidence_shas',evidence,'license_cleared',p.license_cleared));
 RETURN encode(sha256(convert_to(encoded,'UTF8')),'hex');
END $$;
ALTER FUNCTION photo.v27_semantic_policy_sha(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_policy_sha(uuid) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
