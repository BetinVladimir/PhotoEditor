-- Old workers cannot lease a V2 task merely because they qualify for a legacy
-- operation name/backend. The empty default is intentionally fail-closed.
ALTER TABLE photo.worker_registrations ADD COLUMN v2_renderer_versions jsonb NOT NULL DEFAULT '[]'::jsonb
 CHECK (jsonb_typeof(v2_renderer_versions)='array');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_dispatch(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE reserved record; t photo.tasks%ROWTYPE; target uuid; run_kind text;
BEGIN
 SELECT * INTO reserved FROM photo.reserve_dispatch_resources(p_task,p_node,p_ram,p_vram,p_scratch,p_event,p_correlation,p_trace);
 IF reserved.outcome='queued' THEN
  SELECT * INTO t FROM photo.tasks WHERE id=p_task;
  SELECT r.kind INTO run_kind FROM photo.runs r WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id;
  IF run_kind='v2' AND NOT photo.v2_input_closure_ready(p_task) THEN
   RAISE EXCEPTION 'V2_INPUT_CLOSURE_UNSEALED' USING ERRCODE='23514';
  END IF;
  SELECT w.worker_id INTO target FROM photo.worker_registrations w
  WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled AND w.db_principal IS NOT NULL
  AND w.resource_classes ? t.resource_class AND w.backends ? (t.execution_profile->>'backend')
  AND (w.qualified_operations->t.operation) ? (t.execution_profile->>'backend')
  AND w.last_seen>clock_timestamp()-interval '30 seconds'
  AND ((reserved.command->'model')='null'::jsonb OR w.lock_id=reserved.command->'model'->>'lock_id')
  AND (run_kind<>'v2' OR (t.execution_profile->>'renderer_version'='v2.2'
   AND EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation
    AND op.version=t.operation_version AND op.v2_enabled)
   AND w.v2_renderer_versions ? 'v2.2'
   AND EXISTS(SELECT 1 FROM photo.v2_workflows wf WHERE wf.tenant_id=t.tenant_id
    AND wf.run_id=t.run_id AND wf.renderer_version='v2.2')))
  ORDER BY w.worker_id LIMIT 1 FOR SHARE;
  IF target IS NULL THEN RAISE EXCEPTION 'worker changed during admission' USING ERRCODE='40001'; END IF;
  UPDATE photo.tasks SET dispatch_worker_id=target WHERE id=t.id;
  UPDATE photo.outbox SET routing_key='worker.'||target||'.'||routing_key WHERE tenant_id=t.tenant_id AND event_id=p_event;
 END IF;
 RETURN QUERY SELECT reserved.outcome,reserved.command;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; run_kind text;
BEGIN
 t:=photo.lock_task(p_task);
 IF t.id IS NULL THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb;RETURN;
 END IF;
 IF t.dispatch_worker_id IS NOT NULL AND t.dispatch_worker_id IS DISTINCT FROM p_worker THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb;RETURN;
 END IF;
 SELECT r.kind INTO run_kind FROM photo.runs r WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id;
 IF run_kind='v2' AND (
   t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
   OR NOT photo.v2_input_closure_ready(p_task)
   OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation
    AND op.version=t.operation_version AND op.v2_enabled)
   OR NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=t.tenant_id
    AND w.run_id=t.run_id AND w.renderer_version='v2.2')
   OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id
    AND w.worker_id=p_worker AND w.v2_renderer_versions ? 'v2.2')
  ) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb;RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.claim_task_reserved(p_task,p_attempt,p_worker,p_command);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
