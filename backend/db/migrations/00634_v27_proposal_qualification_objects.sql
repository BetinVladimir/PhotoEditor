-- +goose Up
-- Exact document locations after the existing live imported-policy gate.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_proposal_qualification_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb)
RETURNS TABLE(contract text,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.lock_semantic_proposal_qualification(p_tenant,p_project,p_actor,p_session,p_pin) THEN RETURN;END IF;
 RETURN QUERY
 SELECT refs.contract,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes
 FROM (VALUES ('Report'::text,p_pin->'report'),('Calibration'::text,p_pin->'calibration')) refs(contract,ref)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(refs.ref->>'artifact_id')::uuid AND a.sha256=refs.ref->>'sha256'
 ORDER BY refs.contract;
END $$;
ALTER FUNCTION photo.semantic_proposal_qualification_objects(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_proposal_qualification_objects(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_proposal_qualification_objects(uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
