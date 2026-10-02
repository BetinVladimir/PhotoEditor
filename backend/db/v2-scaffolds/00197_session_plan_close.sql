-- Close the session expansion only after the plan task's transport closure
-- proves every adopted descriptor is among its exact typed inputs.
-- +goose StatementBegin
CREATE FUNCTION photo.close_v22_session_plan(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 t photo.tasks%ROWTYPE; descriptor record; descriptor_count integer;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_task IS NULL THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant
  AND run_id=p_run AND id=p_expansion FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.graph_sealed
  OR r.cancel_requested OR e.id IS NULL OR e.kind<>'v2.session.plan/1'
  OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token
  OR e.expansion_lease_until<=clock_timestamp() OR t.id IS NULL
  OR t.node_key<>'session.plan:0' OR t.operation<>'session.plan'
  OR t.operation_version<>'1' OR t.resource_class<>'cpu'
  OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested
  OR NOT photo.v2_input_closure_ready(p_task)
  OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
   AND w.run_id=p_run AND w.workflow_type='session' AND w.renderer_version='v2.2')
  OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation='session.plan'
   AND op.version='1' AND op.resource_class='cpu' AND op.v2_enabled
   AND op.backends ? 'cpu') THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_expansion
   AND d.purpose='v2_session_plan_params' AND d.state='ready'
   AND d.artifact_id=t.params_artifact_id)
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task
    AND i.artifact_id=t.params_artifact_id AND i.contract='Params')
  OR (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='SessionPlanContext')<>1 THEN RETURN false; END IF;
 SELECT count(*) INTO descriptor_count FROM photo.v22_session_plan_inputs(p_tenant,p_run);
 IF descriptor_count NOT BETWEEN 1 AND 40 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='DescriptorShard')<>descriptor_count
  OR (SELECT count(*) FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
   AND edge.run_id=p_run AND edge.dependent_id=p_task)<>descriptor_count THEN RETURN false; END IF;
 FOR descriptor IN SELECT * FROM photo.v22_session_plan_inputs(p_tenant,p_run) LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=descriptor.artifact_id
   AND i.sha256=descriptor.sha256 AND i.contract='DescriptorShard')
   OR NOT EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
    AND edge.run_id=p_run AND edge.prerequisite_id=descriptor.task_id
    AND edge.dependent_id=p_task AND edge.condition='success') THEN RETURN false; END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM photo.graph_expansions other WHERE other.tenant_id=p_tenant
  AND other.run_id=p_run AND other.id<>p_expansion AND other.state<>'closed') THEN RETURN false; END IF;
 UPDATE photo.graph_expansions SET state='closed',cursor='1',
  expansion_token=NULL,expansion_lease_until=NULL
  WHERE tenant_id=p_tenant AND id=p_expansion AND state='open'
   AND expansion_token=p_token;
 IF NOT FOUND THEN RAISE EXCEPTION 'V22_PLAN_EXPANSION_RACE' USING ERRCODE='40001'; END IF;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_run AND status='running'
   AND NOT cancel_requested AND NOT graph_sealed AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'V22_PLAN_SEAL_RACE' USING ERRCODE='40001'; END IF;
 IF NOT photo.activate_bound_task(p_task,t.input_digest,t.params_artifact_id) THEN
  RAISE EXCEPTION 'V22_PLAN_ACTIVATION_FAILED' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.close_v22_session_plan(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_v22_session_plan(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.close_v22_session_plan(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;
