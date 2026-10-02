-- +goose Up
ALTER TABLE photo.tasks ADD COLUMN dispatch_worker_id uuid;
ALTER TABLE photo.tasks ADD FOREIGN KEY(tenant_id,dispatch_worker_id) REFERENCES photo.worker_registrations(tenant_id,worker_id);
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) RENAME TO reserve_dispatch_resources;
REVOKE ALL ON FUNCTION photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text) FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_dispatch(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,p_event uuid,p_correlation uuid,p_trace text)
RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE reserved record; t photo.tasks%ROWTYPE; target uuid;
BEGIN
 SELECT * INTO reserved FROM photo.reserve_dispatch_resources(p_task,p_node,p_ram,p_vram,p_scratch,p_event,p_correlation,p_trace);
 IF reserved.outcome='queued' THEN
  SELECT * INTO t FROM photo.tasks WHERE id=p_task;
  SELECT w.worker_id INTO target FROM photo.worker_registrations w
  WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled AND w.db_principal IS NOT NULL
  AND w.resource_classes ? t.resource_class AND w.backends ? (t.execution_profile->>'backend')
  AND (w.qualified_operations->t.operation) ? (t.execution_profile->>'backend')
  AND w.last_seen>clock_timestamp()-interval '30 seconds'
  AND ((reserved.command->'model')='null'::jsonb OR w.lock_id=reserved.command->'model'->>'lock_id')
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
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) RENAME TO claim_task_reserved;
REVOKE ALL ON FUNCTION photo.claim_task_reserved(uuid,integer,uuid,jsonb) FROM PUBLIC,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 t:=photo.lock_task(p_task);
 IF t.dispatch_worker_id IS NOT NULL AND t.dispatch_worker_id IS DISTINCT FROM p_worker THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.claim_task_reserved(p_task,p_attempt,p_worker,p_command);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
