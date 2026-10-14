-- +goose Up
-- Authenticated pre-child report metadata. No publication or admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_exposure_review_report_evidence(p_tenant uuid,p_intent uuid,p_actor uuid,p_session uuid,p_revision bigint,p_pin jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE identity record; p photo.v2_qualification_policies%ROWTYPE; report photo.artifacts%ROWTYPE;
BEGIN
 IF p_revision IS NULL OR p_revision<1 OR p_pin IS NULL OR jsonb_typeof(p_pin)<>'object'
 OR octet_length(p_pin::text)>16384 THEN RETURN NULL;END IF;
 SELECT * INTO identity FROM photo.semantic_review_identity(p_tenant,p_intent,p_actor,p_session);
 IF identity.run_id IS NULL THEN RETURN NULL;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=identity.run_id
 AND revision=p_revision AND status='waiting_review' AND NOT cancel_requested;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF NOT photo.v27_downstream_qualification_pin_ready(p_tenant,identity.project_id,'semantic.local_exposure','1','cpu',p_pin) THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(p_pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(p_pin->'report'->>'artifact_id')::uuid;
 RETURN jsonb_build_object('pin',p_pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.semantic_exposure_review_report_evidence(uuid,uuid,uuid,uuid,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_exposure_review_report_evidence(uuid,uuid,uuid,uuid,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_exposure_review_report_evidence(uuid,uuid,uuid,uuid,bigint,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
