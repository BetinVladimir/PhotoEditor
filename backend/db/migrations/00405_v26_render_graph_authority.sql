-- +goose Up
-- Repeat complete frozen task-set/closure authority before generic graph gates.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_activation_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; node jsonb; ordinal integer; task_id uuid;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
  OR jsonb_typeof(r.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array'
  OR jsonb_array_length(r.config_snapshot->'v26_render'->'nodes') NOT BETWEEN 1 AND 5000 THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)
  <>jsonb_array_length(r.config_snapshot->'v26_render'->'nodes') THEN RETURN false;END IF;
 FOR ordinal IN 0..jsonb_array_length(r.config_snapshot->'v26_render'->'nodes')-1 LOOP
  node:=r.config_snapshot->'v26_render'->'nodes'->ordinal;
  task_id:=(node->>'task_id')::uuid;
  PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task_id
   AND state='blocked' AND attempt=0 AND NOT cancel_requested FOR UPDATE;
  IF NOT FOUND OR NOT photo.v2_input_closure_ready(task_id) THEN RETURN false;END IF;
  IF NOT photo.stage_v26_render_recipe_task(p_tenant,p_run,task_id,ordinal,
   (node->'params'->>'artifact_id')::uuid,node->'params'->>'sha256',node->>'input_digest',node->'profile') THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_activation_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_activation_ready(uuid,uuid) FROM PUBLIC;
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
  IF NOT photo.v26_render_activation_ready(p_tenant,p_run) THEN RETURN false;END IF;
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
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
