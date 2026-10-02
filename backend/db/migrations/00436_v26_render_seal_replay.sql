-- +goose Up
-- Preserve the core acknowledgement for already activated render graphs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_workflow_id uuid; v_project_id uuid; link record;
 current_status text; current_epoch bigint; v_render_sealed boolean;
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
  SELECT graph_sealed INTO v_render_sealed FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
  -- Already sealed active graphs are acknowledged by the core without mutation.
  -- Draft graphs still require both complete inputs and live qualification.
  IF NOT coalesce(v_render_sealed,false) THEN
   IF NOT photo.v26_render_activation_ready(p_tenant,p_run)
    OR NOT photo.v26_render_qualification_ready(p_tenant,p_run) THEN RETURN false;END IF;
  END IF;
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
