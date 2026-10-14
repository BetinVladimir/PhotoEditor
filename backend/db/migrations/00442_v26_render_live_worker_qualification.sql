-- +goose Up
-- Admission is DB-only; these locks never span provider/GPU execution.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_live_qualification_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; project uuid; pins record; node jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 SELECT w.project_id INTO project FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 IF r.id IS NULL OR project IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
 OR r.status NOT IN('queued','running') OR r.v26_qualification_pin IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='render.compose' AND version='1' FOR SHARE;
 PERFORM id FROM photo.v2_qualification_policies WHERE operation='render.compose' AND operation_version='1' AND admission_enabled FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM report.id FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=project
 AND q.operation=policy.operation AND q.operation_version=policy.operation_version
 AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
 AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
 AND q.trusted_import AND q.status='passed'
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.operation='render.compose' AND policy.operation_version='1' AND policy.admission_enabled
 AND report.state='ready' AND report.media_type='application/json' AND report.verified_at IS NOT NULL
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
 AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 ORDER BY report.id LIMIT 1 FOR SHARE OF report,q;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO pins FROM photo.resolve_v26_render_qualification(p_tenant,project);
 IF NOT FOUND OR r.v26_qualification_pin->>'model_lock_sha' IS DISTINCT FROM pins.model_lock_sha
 OR r.v26_qualification_pin->>'runtime_lock_sha' IS DISTINCT FROM pins.runtime_lock_sha
 OR r.v26_qualification_pin->>'backend' IS DISTINCT FROM pins.backend
 OR r.v26_qualification_pin->>'device_profile_sha' IS DISTINCT FROM pins.device_profile_sha
 OR r.v26_qualification_pin->>'dataset_sha' IS DISTINCT FROM pins.dataset_sha THEN RETURN false;END IF;
 IF jsonb_typeof(r.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array'
 OR jsonb_array_length(r.config_snapshot->'v26_render'->'nodes') NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') LOOP
  IF node->'profile'->>'backend' IS DISTINCT FROM pins.backend
  OR node->'profile'->>'render_runtime_sha' IS DISTINCT FROM pins.runtime_lock_sha
  OR ((node->>'params_jcs')::jsonb)->'scalars'->>'model_lock_sha' IS DISTINCT FROM pins.model_lock_sha THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v26_render_live_qualification_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_live_qualification_ready(uuid,uuid) FROM PUBLIC;

ALTER FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) RENAME TO admit_v26_render_recipe_before_live_qualification;
REVOKE ALL ON FUNCTION photo.admit_v26_render_recipe_before_live_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker;
CREATE FUNCTION photo.admit_v26_render_recipe(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.admit_v26_render_recipe_before_live_qualification(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) THEN RETURN false;END IF;
 RETURN photo.v26_render_live_qualification_ready(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.admit_v26_render_recipe(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
