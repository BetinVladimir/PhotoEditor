-- +goose Up
-- Include shown-retouch copies in V2 CPU worker selection, avoiding an
-- older registration when another qualified V2 CPU worker exists on this node.
-- Keep this predecessor private; the public own dispatch fences still apply.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_dispatch_before_retouch_preview(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
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
  AND (t.operation NOT IN('semantic.preview_retouch_region','semantic.adopt_shown_retouch_region') OR
   (t.operation_version='1' AND t.resource_class='cpu'
    AND t.execution_profile->>'backend'='cpu'
    AND t.execution_profile->>'renderer_version'='v2.2'
    AND w.v2_renderer_versions ? 'v2.2'))
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
ALTER FUNCTION photo.reserve_dispatch_before_retouch_preview(uuid,text,bigint,bigint,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_dispatch_before_retouch_preview(uuid,text,bigint,bigint,bigint,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
