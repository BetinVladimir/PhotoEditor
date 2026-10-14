-- +goose Up
-- Leased negative report/calibration evidence; no object IO in SQL.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_worker_negative_qualification_evidence(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb;p photo.v2_qualification_policies%ROWTYPE;report photo.artifacts%ROWTYPE;
 calibration photo.artifacts%ROWTYPE;c jsonb;answer jsonb;previous_tenant text;
BEGIN
 IF photo.v29_worker_negative_source_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT execution_profile->'negative_qualification' INTO pin FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=p_run;
 SELECT jsonb_build_object('project_id',w.project_id,'actor_id',r.created_by,
 'auth_session_id',r.config_snapshot->'v29_negative'->>'auth_session_id') INTO c
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run;
 IF c IS NULL OR pin IS NULL THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF photo.lock_v29_negative_qualification(p_tenant,(c->>'project_id')::uuid,(c->>'actor_id')::uuid,(c->>'auth_session_id')::uuid,pin) IS DISTINCT FROM true THEN
  PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN NULL;END IF;
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
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
 IF photo.v29_negative_lease_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN answer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_worker_negative_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_worker_negative_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_worker_negative_qualification_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
