-- +goose Up
-- Retained original report/calibration locations must still match under locks.
-- Does not replace content verification outside the transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_donor_qualification_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb,p_objects jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF jsonb_typeof(p_objects) IS DISTINCT FROM 'array' OR jsonb_array_length(p_objects)<>2 OR pg_column_size(p_objects)>32768 THEN RETURN false; END IF;
 IF photo.lock_v28_donor_qualification(p_tenant,p_project,p_actor,p_session,p_pin) IS NOT TRUE THEN RETURN false; END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',refs.contract,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes) ORDER BY refs.contract)
 INTO actual
 FROM (VALUES ('Report'::text,p_pin->'report'),('Calibration'::text,p_pin->'calibration')) refs(contract,ref)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(refs.ref->>'artifact_id')::uuid AND a.sha256=refs.ref->>'sha256';
 RETURN actual IS NOT NULL AND actual=p_objects;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_v28_donor_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_donor_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_donor_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
