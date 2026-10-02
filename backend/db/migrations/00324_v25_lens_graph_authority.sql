-- +goose Up
-- Repeat lens authority/provenance checks in the graph seal transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_activation_ready(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; session_id uuid;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.cancel_requested OR w.renderer_version<>'v2.2'
 OR coalesce(r.config_snapshot->'v25_lens'->>'auth_session_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 THEN RETURN false; END IF;
 session_id:=(r.config_snapshot->'v25_lens'->>'auth_session_id')::uuid;
 -- Hold authority and selected catalog rows until the enclosing seal commits.
 PERFORM id FROM photo.sessions WHERE id=session_id AND user_id=r.created_by FOR SHARE;
 PERFORM id FROM photo.users WHERE id=r.created_by FOR SHARE;
 PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=r.created_by FOR SHARE;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id FOR SHARE;
 PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=w.project_id AND user_id=r.created_by FOR SHARE;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id FOR SHARE;
 PERFORM v.id FROM photo.snapshot_items i
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id
 JOIN photo.artifacts original ON original.tenant_id=a.tenant_id AND original.id=a.original_artifact_id
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id
 ORDER BY v.id FOR SHARE OF v,a,source,original;
 IF NOT photo.v25_lens_snapshot_ready(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,session_id)
 OR NOT photo.v25_lens_plan_matches_snapshot(p_tenant,r.input_snapshot_id,w.id,r.config_snapshot) THEN RETURN false; END IF;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)
 <>jsonb_array_length(r.config_snapshot->'v25_lens'->'nodes') THEN RETURN false; END IF;
 RETURN NOT EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'v25_lens'->'nodes') n
 WHERE NOT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id::text=n->>'task_id'
 AND t.operation='optical.lens' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.node_key=(SELECT 'optical.lens:'||i.ordinal::text FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant
  AND i.snapshot_id=r.input_snapshot_id AND i.asset_version_id::text=n->'params'->'base'->>'asset_version_id')
 AND params.sha256=n->>'params_sha'
 AND (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=n->'profile'));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_activation_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_activation_ready(uuid,uuid) FROM PUBLIC;
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
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
