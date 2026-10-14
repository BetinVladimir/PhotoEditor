-- +goose Up
-- Correct project binding to the immutable Apply config snapshot; malformed scope denies.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_exposure_worker_report_evidence(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; p photo.v2_qualification_policies%ROWTYPE; report photo.artifacts%ROWTYPE;
BEGIN
 IF NOT photo.admit_semantic_local_exposure(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) THEN RETURN NULL;END IF;
 SELECT t.execution_profile->'qualification' INTO pin
 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task;
 IF NOT photo.v27_downstream_profile_ready(p_tenant,
 (SELECT (config_snapshot->>'project_id')::uuid FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run),
 'semantic.local_exposure','1','cpu',
 (SELECT execution_profile FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) THEN RETURN NULL;END IF;
 IF pin IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO p FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid;
 SELECT * INTO report FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(pin->'report'->>'artifact_id')::uuid;
 RETURN jsonb_build_object('pin',pin,'policy',jsonb_build_object(
 'schema_version',1,'policy_id',p.id,'operation',p.operation,'operation_version',p.operation_version,'backend',p.backend,
 'model_lock_sha',p.model_lock_sha,'runtime_lock_sha',p.runtime_lock_sha,'device_profile_sha',p.device_profile_sha,
 'dataset_sha',p.dataset_sha,'issuer_id',p.issuer_id,'required_checks',p.required_checks,'test_fixture_shas',p.test_fixture_shas,
 'calibration_shas',p.calibration_shas,'evidence_shas',p.evidence_shas,'license_cleared',p.license_cleared),
 'report',jsonb_build_object('artifact_id',report.id,'sha256',report.sha256,'bucket',report.bucket,
 'object_key',report.object_key,'object_version',report.object_version,'bytes',report.bytes,'media_type',report.media_type));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v27_exposure_worker_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_exposure_worker_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_exposure_worker_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
