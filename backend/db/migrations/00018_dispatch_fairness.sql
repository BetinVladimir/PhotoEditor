-- +goose Up
ALTER TABLE photo.tasks ADD COLUMN admission_after timestamptz;
ALTER TABLE photo.outbox ADD COLUMN admission_accounted boolean NOT NULL DEFAULT false;
CREATE INDEX task_admission_ready ON photo.tasks(tenant_id,admission_after,priority,created_at,id) WHERE state='ready';
-- +goose StatementBegin
CREATE FUNCTION photo.pick_ready_task() RETURNS TABLE(task_id uuid,resource_class text,profile jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.scheduler_tenants%ROWTYPE;
BEGIN
 INSERT INTO photo.scheduler_tenants(tenant_id)
 SELECT DISTINCT t.tenant_id FROM photo.tasks t WHERE t.state='ready' AND NOT EXISTS(SELECT FROM photo.scheduler_tenants s WHERE s.tenant_id=t.tenant_id) ON CONFLICT(tenant_id) DO NOTHING;
 SELECT s.* INTO tenant FROM photo.scheduler_tenants s WHERE EXISTS(
  SELECT FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
  WHERE t.tenant_id=s.tenant_id AND t.state='ready' AND NOT t.cancel_requested AND NOT r.cancel_requested AND r.graph_sealed
  AND r.status IN ('queued','running') AND (t.admission_after IS NULL OR t.admission_after<=clock_timestamp()))
 ORDER BY s.virtual_finish,s.tenant_id LIMIT 1 FOR UPDATE OF s SKIP LOCKED;
 IF NOT FOUND THEN RETURN; END IF;
 RETURN QUERY SELECT t.id,t.resource_class,t.execution_profile FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 WHERE t.tenant_id=tenant.tenant_id AND t.state='ready' AND NOT t.cancel_requested AND NOT r.cancel_requested AND r.graph_sealed
 AND r.status IN ('queued','running') AND (t.admission_after IS NULL OR t.admission_after<=clock_timestamp())
 ORDER BY CASE WHEN tenant.preview_streak>=5 THEN CASE WHEN t.priority<=0 THEN 0 ELSE 1 END ELSE CASE WHEN t.priority>0 THEN 0 ELSE 1 END END,t.created_at,t.id LIMIT 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pick_ready_task() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pick_ready_task() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pick_ready_task() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.eligible_dispatch_nodes(p_task uuid) RETURNS TABLE(node_id text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT n.node_id FROM photo.resource_nodes n JOIN photo.tasks t ON t.id=p_task
 WHERE n.enabled AND n.reported_at>clock_timestamp()-interval '30 seconds' AND n.doctor_report_sha256 IS NOT NULL
 AND EXISTS(SELECT FROM photo.resource_slots s WHERE s.node_id=n.node_id AND s.resource_class=t.resource_class AND s.task_id IS NULL)
 AND EXISTS(SELECT FROM photo.worker_registrations w WHERE w.node_id=n.node_id AND w.tenant_id=t.tenant_id AND w.enabled
 AND w.resource_classes ? t.resource_class AND (w.qualified_operations->t.operation) ? (t.execution_profile->>'backend') AND w.backends ? (t.execution_profile->>'backend') AND w.last_seen>clock_timestamp()-interval '30 seconds')
 ORDER BY (SELECT count(*) FROM photo.resource_reservations r WHERE r.node_id=n.node_id AND r.state='reserved'),n.node_id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.eligible_dispatch_nodes(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.eligible_dispatch_nodes(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.eligible_dispatch_nodes(uuid) TO photo_scheduler;

-- Called in the same transaction that owns pick_ready_task's tenant row lock.
-- +goose StatementBegin
CREATE FUNCTION photo.record_dispatch(p_task uuid,p_event uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task AND state='queued' AND (dispatch_command->>'event_id')::uuid=p_event;
 IF NOT FOUND THEN RETURN false; END IF;
 UPDATE photo.outbox SET admission_accounted=true WHERE tenant_id=t.tenant_id AND event_id=p_event AND NOT admission_accounted;
 IF NOT FOUND THEN RETURN EXISTS(SELECT FROM photo.outbox WHERE tenant_id=t.tenant_id AND event_id=p_event AND admission_accounted); END IF;
 UPDATE photo.scheduler_tenants SET virtual_finish=virtual_finish+CASE t.resource_class WHEN 'media' THEN 1 WHEN 'cpu' THEN 2 WHEN 'archive' THEN 3 WHEN 'gpu' THEN 5 ELSE 2 END,
 last_dispatch_at=clock_timestamp(),preview_streak=CASE WHEN t.priority>0 THEN least(preview_streak+1,5) ELSE 0 END WHERE tenant_id=t.tenant_id;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.record_dispatch(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_dispatch(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_dispatch(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.defer_dispatch(p_task uuid,p_reason text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 IF p_reason IS NULL OR p_reason NOT IN ('waiting_resource','waiting_worker','waiting_quota','unsupported_backend','unqualified_model','input_unavailable','invalid_resource_estimate') THEN
 RAISE EXCEPTION 'INVALID_ADMISSION_REASON' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR t.state<>'ready' THEN RETURN false; END IF;
 UPDATE photo.tasks SET admission_after=clock_timestamp()+interval '2 seconds',terminal_reason=p_reason WHERE id=t.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.defer_dispatch(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.defer_dispatch(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.defer_dispatch(uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
