-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v28_local_qualification_pin jsonb;
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v28;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v28(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;v_project_id uuid;runtime_sha text;pin jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF r.config_snapshot ? 'v28_local_portrait' OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation IN('portrait.hair','portrait.clothing')) THEN
  -- A replay cannot activate additional work; delegate prior sealed-state rules.
  IF r.graph_sealed THEN
   IF r.v28_local_qualification_pin IS NULL THEN RETURN false;END IF;
   RETURN photo.seal_v2_graph_pre_v28(p_tenant,p_run);
  END IF;
  IF r.status<>'draft' OR r.cancel_requested OR r.v28_local_task_plan IS NULL
   OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>1 THEN RETURN false;END IF;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
   AND id=(r.v28_local_task_plan->'task'->>'id')::uuid FOR UPDATE;
  SELECT w.project_id INTO v_project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
  IF t.id IS NULL OR v_project_id IS NULL OR t.operation NOT IN('portrait.hair','portrait.clothing')
   OR photo.v28_local_registered_inventory_ready(p_tenant,p_run,t.id) IS DISTINCT FROM true
   OR photo.v2_input_closure_ready(t.id) IS DISTINCT FROM true THEN RETURN false;END IF;
  runtime_sha:=t.execution_profile->>'portrait_runtime_sha';
  IF photo.v28_local_runtime_qualification_ready(p_tenant,v_project_id,t.operation,runtime_sha) IS DISTINCT FROM true THEN RETURN false;END IF;
  SELECT jsonb_build_object('policy_id',policy.id,'qualification_id',q.id,'policy_sha',photo.v28_local_policy_sha(policy.id),
   'operation',t.operation,'operation_version','1','backend','cpu','model_lock_sha',policy.model_lock_sha,
   'runtime_lock_sha',policy.runtime_lock_sha,'device_profile_sha',policy.device_profile_sha,'dataset_sha',policy.dataset_sha,
   'issuer_id',policy.issuer_id,'report',jsonb_build_object('artifact_id',q.report_artifact_id,'sha256',q.report_sha))
  INTO pin FROM photo.v2_qualification_policies policy JOIN photo.model_qualifications q
   ON q.tenant_id=p_tenant AND q.project_id=v_project_id AND q.operation=policy.operation AND q.operation_version=policy.operation_version
   AND q.backend=policy.backend AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
   AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
  JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
  WHERE policy.operation=t.operation AND policy.operation_version='1' AND policy.backend='cpu' AND policy.runtime_lock_sha=runtime_sha
   AND policy.enabled AND policy.admission_enabled AND policy.license_cleared AND q.trusted_import AND q.status='passed'
   AND report.state='ready' AND report.verified_at IS NOT NULL AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
   AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608 AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.audit_log log WHERE log.tenant_id=q.tenant_id AND log.action='v2_qualification.imported'
    AND log.resource_type='model_qualification' AND log.resource_id=q.id AND log.details->>'policy_id'=policy.id::text)
  ORDER BY policy.id,q.id LIMIT 1;
  IF pin IS NULL THEN RETURN false;END IF;
  IF r.v28_local_qualification_pin IS NOT NULL AND r.v28_local_qualification_pin IS DISTINCT FROM pin THEN RETURN false;END IF;
  -- Delegate all existing registry/contracts/quota/activation gates first.
  IF photo.seal_v2_graph_pre_v28(p_tenant,p_run) IS DISTINCT FROM true THEN RETURN false;END IF;
  UPDATE photo.runs SET v28_local_qualification_pin=pin WHERE tenant_id=p_tenant AND id=p_run;
  RETURN true;
 END IF;
 RETURN photo.seal_v2_graph_pre_v28(p_tenant,p_run);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
