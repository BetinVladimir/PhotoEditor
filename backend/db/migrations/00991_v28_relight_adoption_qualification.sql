-- +goose Up
-- Completed scheduler qualification catalog; original object IO remains outside SQL.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_adoption_qualification(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;again jsonb;pin jsonb;p photo.v2_qualification_policies%ROWTYPE;
 report photo.artifacts%ROWTYPE;calibration photo.artifacts%ROWTYPE;answer jsonb;
BEGIN
 source:=photo.v28_relight_adoption_source(p_tenant,p_task,p_attempt,p_fence,p_result,p_body);
 IF source IS NULL THEN RETURN NULL;END IF;
 pin:=source->'qualification';
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'report'->>'artifact_id')::uuid;
 SELECT * INTO calibration FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'calibration'->>'artifact_id')::uuid;
 answer:=jsonb_build_object('pin',pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type),
 'calibration',jsonb_build_object('artifact_id',calibration.id,'sha256',calibration.sha256,'bucket',calibration.bucket,
 'object_key',calibration.object_key,'object_version',calibration.object_version,'bytes',calibration.bytes,'media_type',calibration.media_type));
 again:=photo.v28_relight_adoption_source(p_tenant,p_task,p_attempt,p_fence,p_result,p_body);
 IF again IS DISTINCT FROM source THEN RETURN NULL;END IF;
 RETURN answer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_adoption_qualification(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_adoption_qualification(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_adoption_qualification(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
