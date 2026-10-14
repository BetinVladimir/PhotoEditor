-- +goose Up
-- Source locators are returned only after live policy/report/calibration locking.
-- Caller commits this short transaction before object IO and rechecks at admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_qualification_sources(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF photo.lock_v29_negative_qualification(p_tenant,p_project,p_actor,p_session,p_pin) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',refs.contract,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes) ORDER BY refs.contract)
 INTO actual
 FROM (VALUES ('QualificationReport'::text,p_pin->'report'),('NegativeCalibration'::text,p_pin->'calibration')) refs(contract,ref)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(refs.ref->>'artifact_id')::uuid AND a.sha256=refs.ref->>'sha256';
 IF jsonb_array_length(actual) IS DISTINCT FROM 2 THEN RETURN NULL;END IF;
 RETURN actual;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_negative_qualification_sources(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_qualification_sources(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_negative_qualification_sources(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
