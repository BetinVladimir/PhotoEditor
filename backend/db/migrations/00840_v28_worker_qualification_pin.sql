-- +goose Up
-- Bind every live worker gate to the exact qualification captured at seal.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_qualification_pin_ready(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb;t photo.tasks%ROWTYPE;project uuid;
BEGIN
 SELECT r.v28_local_qualification_pin INTO pin FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR SHARE;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 IF t.id IS NULL OR project IS NULL OR jsonb_typeof(pin) IS DISTINCT FROM 'object'
  OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(pin) k) IS DISTINCT FROM
   ARRAY['backend','dataset_sha','device_profile_sha','issuer_id','model_lock_sha','operation','operation_version','policy_id','policy_sha','qualification_id','report','runtime_lock_sha']::text[]
  OR pin->>'operation' IS DISTINCT FROM t.operation OR pin->>'operation_version' IS DISTINCT FROM '1'
  OR pin->>'backend' IS DISTINCT FROM 'cpu' OR pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'portrait_runtime_sha'
  OR photo.v25_layer_ref_shape(pin->'report') IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.v28_local_runtime_qualification_ready(p_tenant,project,t.operation,pin->>'runtime_lock_sha') IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM policy.id FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=project AND q.id=(pin->>'qualification_id')::uuid
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.id=(pin->>'policy_id')::uuid AND policy.operation=t.operation AND policy.operation_version='1' AND policy.backend='cpu'
  AND policy.enabled AND policy.admission_enabled AND policy.license_cleared
  AND policy.model_lock_sha=pin->>'model_lock_sha' AND policy.runtime_lock_sha=pin->>'runtime_lock_sha'
  AND policy.device_profile_sha=pin->>'device_profile_sha' AND policy.dataset_sha=pin->>'dataset_sha'
  AND policy.issuer_id::text=pin->>'issuer_id' AND photo.v28_local_policy_sha(policy.id)=pin->>'policy_sha'
  AND q.operation=policy.operation AND q.operation_version=policy.operation_version AND q.backend=policy.backend
  AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
  AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha AND q.trusted_import AND q.status='passed'
  AND report.id::text=pin->'report'->>'artifact_id' AND report.sha256=pin->'report'->>'sha256'
  AND report.state='ready' AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
  AND report.verified_at IS NOT NULL AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
  AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
   AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
 FOR SHARE OF policy,q,report;
 RETURN FOUND;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v28_local_qualification_pin_ready(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_qualification_pin_ready(uuid,uuid,uuid) FROM PUBLIC;
ALTER FUNCTION photo.v28_local_portrait_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) RENAME TO v28_local_portrait_lease_ready_pre_qualification;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_lease_ready_pre_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker;
CREATE FUNCTION photo.v28_local_portrait_lease_ready(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v28_local_portrait_lease_ready_pre_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.v28_local_qualification_pin_ready(p_tenant,p_run,p_task);
END $$;
ALTER FUNCTION photo.v28_local_portrait_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_local_portrait_lease_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
