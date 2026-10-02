-- Preserve the V21 adoption body as a private core and place the consent
-- epoch lock before its run/task locks. Existing callers keep the same API.
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_core;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_core(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM photo_scheduler;

-- The last adopted descriptor atomically opens the late-bound plan barrier.
-- The caller already owns the run lock in adopt_v2_plan_core. Finalization
-- cannot observe all adopted descriptors with a sealed graph between these
-- statements because both changes commit together.
-- +goose StatementBegin
CREATE FUNCTION photo.open_v22_session_plan_barrier(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_run uuid; v_count integer;
BEGIN
 SELECT t.run_id INTO v_run FROM photo.tasks t
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='session.describe'
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND r.kind='v2' AND r.status='running' AND r.graph_sealed
  AND NOT r.cancel_requested;
 IF v_run IS NULL THEN RETURN false; END IF;
 SELECT count(*) INTO v_count FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=v_run AND t.operation='session.describe';
 IF v_count NOT BETWEEN 1 AND 40 OR EXISTS(
  SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=v_run
   AND t.operation NOT IN ('session.describe','session.plan')) THEN
  RAISE EXCEPTION 'V22_SESSION_GRAPH_INVALID' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=v_run AND t.operation='session.plan') OR EXISTS(
  SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant
   AND e.run_id=v_run AND e.kind='v2.session.plan/1') THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=v_run AND t.operation='session.describe' AND
  (t.state<>'succeeded' OR t.canonical_result_id IS NULL OR NOT EXISTS(
   SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
    AND i.consumer='v2-adopt' AND
    i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)))) THEN RETURN false; END IF;
 INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,kind,state,expected_count)
 VALUES(p_tenant,v_run,p_task,'v2.session.plan/1','open',1);
 UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=p_tenant AND id=v_run
  AND status='running' AND graph_sealed AND NOT cancel_requested;
 IF NOT FOUND THEN RAISE EXCEPTION 'V22_SESSION_BARRIER_RACE' USING ERRCODE='40001'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.open_v22_session_plan_barrier(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.open_v22_session_plan_barrier(uuid,uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_run_id uuid; v_workflow_id uuid; v_project_id uuid; link record;
 current_status text; current_epoch bigint; v_adopted boolean;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT t.run_id,w.id,w.project_id INTO v_run_id,v_workflow_id,v_project_id
  FROM photo.tasks t JOIN photo.v2_workflows w
   ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
  WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF v_workflow_id IS NULL THEN RETURN false; END IF;
 FOR link IN SELECT l.consent_id,l.consent_epoch FROM photo.workflow_consent_links l
  WHERE l.tenant_id=p_tenant AND l.project_id=v_project_id AND l.workflow_id=v_workflow_id
  ORDER BY l.consent_id LOOP
  SELECT g.status,g.epoch INTO current_status,current_epoch FROM photo.consent_grants g
   WHERE g.tenant_id=p_tenant AND g.project_id=v_project_id AND g.id=link.consent_id
   FOR UPDATE;
  IF current_status IS DISTINCT FROM 'granted' OR
   current_epoch IS DISTINCT FROM link.consent_epoch THEN RETURN false; END IF;
 END LOOP;
 v_adopted:=photo.adopt_v2_plan_core(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
 IF v_adopted THEN PERFORM photo.open_v22_session_plan_barrier(p_tenant,p_task); END IF;
 RETURN v_adopted;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 TO photo_scheduler;

-- Seal must perform the same epoch check: a revoked draft cannot enter queue.
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_core;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_core(uuid,uuid) FROM photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_workflow_id uuid; v_project_id uuid; link record;
 current_status text; current_epoch bigint;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT w.id,w.project_id INTO v_workflow_id,v_project_id FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
 IF v_workflow_id IS NULL THEN RETURN false; END IF;
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
