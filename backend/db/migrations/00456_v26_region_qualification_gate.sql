-- +goose Up
-- New calibrated profiles retain/recheck live qualification at seal, worker
-- admission (which calls seal scope), and final adoption. Existing uncalibrated
-- provider profiles retain their prior contract; default Region stays disabled.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_qualification_pin_ready(
 p_tenant uuid,p_project uuid,p_policy_sha text,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin jsonb; q jsonb; sample jsonb; peak jsonb; live record; key text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 pin:=p_profile->'region_qualification';q:=pin->'qualification';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' OR jsonb_typeof(q) IS DISTINCT FROM 'object'
  OR pin-'qualification'-'catalog_sha'-'calibration_sha'-'workload_sha'-'evidence_sha'-'sample'<>'{}'::jsonb
  OR q-'runtime_lock_sha'-'model_lock_sha'-'icc_lock_sha'-'device_profile_sha'-'dataset_sha'-'policy_sha'-'backend'<>'{}'::jsonb
  OR q->>'backend' IS DISTINCT FROM 'cuda' THEN RETURN false;END IF;
 FOREACH key IN ARRAY ARRAY['catalog_sha','calibration_sha','workload_sha','evidence_sha'] LOOP
  IF jsonb_typeof(pin->key) IS DISTINCT FROM 'string' OR coalesce(pin->>key,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 FOREACH key IN ARRAY ARRAY['runtime_lock_sha','model_lock_sha','icc_lock_sha','device_profile_sha','dataset_sha','policy_sha'] LOOP
  IF jsonb_typeof(q->key) IS DISTINCT FROM 'string' OR coalesce(q->>key,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 IF p_profile->>'region_runtime_sha256' IS DISTINCT FROM q->>'runtime_lock_sha'
  OR p_profile->>'model_lock_sha' IS DISTINCT FROM q->>'model_lock_sha'
  OR p_profile->>'icc_lock_sha' IS DISTINCT FROM q->>'icc_lock_sha'
  OR p_profile->>'backend' IS DISTINCT FROM q->>'backend'
  OR p_policy_sha IS DISTINCT FROM q->>'policy_sha' THEN RETURN false;END IF;
 sample:=pin->'sample';peak:=sample->'operator_peak';
 IF jsonb_typeof(sample) IS DISTINCT FROM 'object' OR jsonb_typeof(peak) IS DISTINCT FROM 'object'
  OR sample-'width'-'height'-'operator_peak'<>'{}'::jsonb
  OR peak-'ram_bytes'-'scratch_bytes'-'vram_bytes'-'output_bytes'<>'{}'::jsonb THEN RETURN false;END IF;
 FOREACH key IN ARRAY ARRAY['width','height'] LOOP
  IF jsonb_typeof(sample->key) IS DISTINCT FROM 'number' OR coalesce(sample->>key,'') !~ '^[0-9]+$'
   OR (sample->>key)::numeric NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 END LOOP;
 IF (sample->>'width')::numeric*(sample->>'height')::numeric>200000000 THEN RETURN false;END IF;
 FOREACH key IN ARRAY ARRAY['ram_bytes','scratch_bytes','vram_bytes','output_bytes'] LOOP
  IF jsonb_typeof(peak->key) IS DISTINCT FROM 'number' OR coalesce(peak->>key,'') !~ '^[0-9]+$'
   OR (peak->>key)::numeric NOT BETWEEN 0 AND 9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 IF (peak->>'vram_bytes')::numeric<1
  OR p_profile->'resource_estimate'->'vram_bytes' IS DISTINCT FROM peak->'vram_bytes' THEN RETURN false;END IF;
 -- Keep operation/policy/report authority stable until caller commits.
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='region.preview' AND version='1' FOR SHARE;
 PERFORM id FROM photo.v2_qualification_policies WHERE operation='region.preview' AND operation_version='1' AND admission_enabled FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM report.id FROM photo.model_qualifications qualified
 JOIN photo.artifacts report ON report.tenant_id=qualified.tenant_id AND report.id=qualified.report_artifact_id AND report.sha256=qualified.report_sha
 WHERE qualified.tenant_id=p_tenant AND qualified.project_id=p_project AND qualified.operation='region.preview' AND qualified.operation_version='1'
  AND qualified.runtime_lock_sha=q->>'runtime_lock_sha' AND qualified.model_lock_sha=q->>'model_lock_sha'
  AND qualified.device_profile_sha=q->>'device_profile_sha' AND qualified.dataset_sha=q->>'dataset_sha' AND qualified.backend='cuda'
  AND qualified.trusted_import AND qualified.status='passed'
 ORDER BY report.id FOR SHARE OF qualified,report;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO live FROM photo.resolve_v26_region_qualification(p_tenant,p_project,pin->>'catalog_sha',pin->>'evidence_sha');
 RETURN FOUND AND q=jsonb_build_object('runtime_lock_sha',live.runtime_lock_sha,'model_lock_sha',live.model_lock_sha,
  'icc_lock_sha',live.icc_lock_sha,'device_profile_sha',live.device_profile_sha,'dataset_sha',live.dataset_sha,
  'policy_sha',live.policy_sha,'backend',live.backend);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.v26_region_qualification_pin_ready(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_qualification_pin_ready(uuid,uuid,text,jsonb) FROM PUBLIC;

ALTER FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) RENAME TO v26_region_seal_scope_pre_qualification;
REVOKE ALL ON FUNCTION photo.v26_region_seal_scope_pre_qualification(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC,photo_api;
CREATE FUNCTION photo.v26_region_seal_scope(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_node jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow photo.v2_workflows%ROWTYPE;
BEGIN
 IF NOT photo.v26_region_seal_scope_pre_qualification(p_tenant,p_run,p_actor,p_session,p_node) THEN RETURN false;END IF;
 IF NOT (p_node->'profile' ? 'region_qualification') THEN RETURN true;END IF;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 RETURN workflow.id IS NOT NULL AND photo.v26_region_qualification_pin_ready(p_tenant,workflow.project_id,workflow.policy_sha,p_node->'profile');
END $$;
ALTER FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) TO photo_api;

ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_region_qualification;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_region_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task photo.tasks%ROWTYPE; run photo.runs%ROWTYPE; workflow photo.v2_workflows%ROWTYPE; node jsonb;
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='region.preview') THEN
  PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT r.* INTO run FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
   WHERE t.tenant_id=p_tenant AND t.id=p_task FOR UPDATE OF r;
  SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
  SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=run.id;
  node:=run.config_snapshot->'v26_region'->'nodes'->0;
  IF node->'profile' ? 'region_qualification' THEN
   PERFORM set_config('app.tenant_id',p_tenant::text,true);
   IF workflow.id IS NULL OR NOT photo.v26_region_qualification_pin_ready(p_tenant,workflow.project_id,workflow.policy_sha,node->'profile') THEN RETURN false;END IF;
  END IF;
 END IF;
 RETURN photo.adopt_v2_plan_pre_region_qualification(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
