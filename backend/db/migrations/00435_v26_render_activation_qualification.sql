-- +goose Up
-- Lock live qualification evidence until graph activation commits.
-- PostgreSQL requires an UPDATE privilege for SELECT FOR SHARE. These identity
-- columns are never written by this function; service roles receive no grants.
GRANT UPDATE(id) ON photo.projects,photo.v2_qualification_policies,photo.model_qualifications TO photo_fn_owner;
GRANT UPDATE(version) ON photo.operation_registry TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_qualification_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; v_project_id uuid; pins record; node jsonb;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT w.project_id INTO v_project_id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 IF r.id IS NULL OR v_project_id IS NULL OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR jsonb_typeof(r.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array'
 OR jsonb_array_length(r.config_snapshot->'v26_render'->'nodes') NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=v_project_id AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation='render.compose' AND version='1' FOR SHARE;
 PERFORM id FROM photo.v2_qualification_policies WHERE operation='render.compose' AND operation_version='1' AND admission_enabled FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM report.id FROM photo.v2_qualification_policies policy
 JOIN photo.model_qualifications q ON q.tenant_id=p_tenant AND q.project_id=v_project_id
  AND q.operation=policy.operation AND q.operation_version=policy.operation_version
  AND q.model_lock_sha=policy.model_lock_sha AND q.runtime_lock_sha=policy.runtime_lock_sha
  AND q.backend=policy.backend AND q.device_profile_sha=policy.device_profile_sha AND q.dataset_sha=policy.dataset_sha
  AND q.trusted_import AND q.status='passed'
 JOIN photo.artifacts report ON report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id AND report.sha256=q.report_sha
 WHERE policy.operation='render.compose' AND policy.operation_version='1' AND policy.admission_enabled
 AND report.state='ready' AND report.media_type='application/json' AND report.verified_at IS NOT NULL
 AND report.object_version IS NOT NULL AND report.object_version NOT IN('','null')
 AND (report.expires_at IS NULL OR report.expires_at>clock_timestamp())
 ORDER BY report.id LIMIT 1
 FOR SHARE OF report,q;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO pins FROM photo.resolve_v26_render_qualification(p_tenant,v_project_id);
 IF NOT FOUND THEN RETURN false;END IF;
 FOR node IN SELECT value FROM jsonb_array_elements(r.config_snapshot->'v26_render'->'nodes') LOOP
  IF node->'profile'->>'backend' IS DISTINCT FROM pins.backend
   OR node->'profile'->>'render_runtime_sha' IS DISTINCT FROM pins.runtime_lock_sha
   OR ((node->>'params_jcs')::jsonb)->'scalars'->>'model_lock_sha' IS DISTINCT FROM pins.model_lock_sha THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_qualification_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_qualification_ready(uuid,uuid) FROM PUBLIC;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_workflow_id uuid; v_project_id uuid; link record;
 current_status text; current_epoch bigint;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT w.id,w.project_id INTO v_workflow_id,v_project_id FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 IF v_workflow_id IS NULL THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.config_snapshot ? 'v25_lens')
 OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='optical.lens') THEN
  PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
  PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
  IF NOT photo.v25_lens_activation_ready(p_tenant,p_run) THEN RETURN false; END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.config_snapshot ? 'v26_render')
 OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='render.compose') THEN
  PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
  PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
  IF NOT photo.v26_render_activation_ready(p_tenant,p_run)
   OR NOT photo.v26_render_qualification_ready(p_tenant,p_run) THEN RETURN false;END IF;
 END IF;
 FOR link IN SELECT l.consent_id,l.consent_epoch FROM photo.workflow_consent_links l
  WHERE l.tenant_id=p_tenant AND l.project_id=v_project_id AND l.workflow_id=v_workflow_id
  ORDER BY l.consent_id LOOP
  SELECT g.status,g.epoch INTO current_status,current_epoch FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=v_project_id AND g.id=link.consent_id
   FOR UPDATE;
  IF current_status IS DISTINCT FROM 'granted' OR
   current_epoch IS DISTINCT FROM link.consent_epoch THEN RETURN false; END IF;
 END LOOP;
 RETURN photo.seal_v2_graph_core(p_tenant,p_run);
END $$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
