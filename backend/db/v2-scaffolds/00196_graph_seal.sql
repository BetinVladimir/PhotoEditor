-- Registration is explicit per package; legacy operations are never V2-capable
-- merely because their resource class and renderer happen to match.
ALTER TABLE photo.operation_registry ADD COLUMN v2_enabled boolean NOT NULL DEFAULT false;

-- Seal a complete, quota-backed graph, then activate only tasks without
-- prerequisites through the existing fenced bound-task activator.
-- +goose StatementBegin
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
 task_count integer; edge_count integer; remaining uuid[]; removable uuid[]; root record;
 next_cursor bigint; event_id uuid; saved_count integer;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL THEN RETURN false; END IF;
 -- Match input-document admission's usage -> run -> task lock order.
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.cancel_requested THEN RETURN false; END IF;
 IF r.graph_sealed THEN RETURN r.status IN ('queued','running','waiting_review'); END IF;
 IF r.status<>'draft' OR r.failure_policy<>'fail_fast' OR
  NOT EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant AND tenant.status='active') OR
  NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run
   AND w.renderer_version='v2.2') OR
  EXISTS(SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run
   AND e.state<>'closed') THEN RETURN false; END IF;
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant AND x.holder_type='run'
  AND x.holder_id=p_run AND x.state='reserved' AND x.expires_at>clock_timestamp() FOR UPDATE;
 IF q.id IS NULL OR q.bytes<=0 OR NOT EXISTS(SELECT 1 FROM photo.tenant_usage u
  WHERE u.tenant_id=p_tenant AND u.reserved_bytes>=q.bytes) THEN RETURN false; END IF;
 SELECT count(*) INTO task_count FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run;
 SELECT count(*) INTO edge_count FROM photo.task_edges e WHERE e.tenant_id=p_tenant AND e.run_id=p_run;
 IF task_count NOT BETWEEN 1 AND 5000 OR edge_count>20000 THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND (
  t.state<>'blocked' OR t.cancel_requested OR t.attempt<>0 OR
  NOT photo.v2_input_closure_ready(t.id) OR
  NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation
   AND op.version=t.operation_version AND op.v2_enabled
   AND op.resource_class=t.resource_class AND op.backends ? (t.execution_profile->>'backend')) OR
  NOT EXISTS(SELECT 1 FROM photo.operation_output_policies policy WHERE
   policy.operation=t.operation AND policy.version=t.operation_version
   AND policy.role='document' AND policy.media_types ? 'application/json'))) THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=p_tenant AND e.run_id=p_run
  AND e.condition<>'success') THEN RETURN false; END IF;
 SELECT array_agg(t.id ORDER BY t.id) INTO remaining FROM photo.tasks t
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run;
 WHILE coalesce(array_length(remaining,1),0)>0 LOOP
  SELECT array_agg(node.id ORDER BY node.id) INTO removable FROM unnest(remaining) node(id)
   WHERE NOT EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=p_tenant
    AND e.run_id=p_run AND e.dependent_id=node.id AND e.prerequisite_id=ANY(remaining));
  IF removable IS NULL THEN RETURN false; END IF;
  SELECT array_agg(node.id ORDER BY node.id) INTO remaining FROM unnest(remaining) node(id)
   WHERE node.id<>ALL(removable);
 END LOOP;
 UPDATE photo.runs SET graph_sealed=true,status='queued',revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_run AND status='draft' AND NOT graph_sealed;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_GRAPH_SEAL_RACE' USING ERRCODE='40001'; END IF;
 FOR root IN SELECT t.id,t.input_digest,t.params_artifact_id FROM photo.tasks t
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND NOT EXISTS(
   SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.dependent_id=t.id)
  ORDER BY t.id LOOP
  IF NOT photo.activate_bound_task(root.id,root.input_digest,root.params_artifact_id) THEN
   RAISE EXCEPTION 'V2_ROOT_ACTIVATION_FAILED' USING ERRCODE='23514';
  END IF;
 END LOOP;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514'; END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','v2.run.queued',
  jsonb_build_object('run_id',p_run,'status','queued','revision',r.revision+1));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.run.queued',
  jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.queued',
   'tenant_id',p_tenant,'run_id',p_run,'status','queued'));
 UPDATE photo.idempotency_records i SET response_status=202,
  response_body=jsonb_build_object('run_id',p_run,'workflow_id',
   (SELECT w.id FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run),
   'state','queued')
 WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=r.created_by
  AND i.route='V2.Admit' AND i.resource_id=p_run AND i.response_status IS NULL;
 GET DIAGNOSTICS saved_count = ROW_COUNT;
 IF saved_count>1 THEN RAISE EXCEPTION 'V2_IDEMPOTENCY_DUPLICATE' USING ERRCODE='23514'; END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
