-- +goose Up
-- One task per transaction preserves node/slot -> usage -> run -> task order.
-- Running attempts cooperate through heartbeat; recovery fences expired leases.
-- +goose StatementBegin
CREATE FUNCTION photo.cancel_idle_task() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid; t photo.tasks%ROWTYPE;
BEGIN
 SELECT q.id INTO candidate FROM photo.tasks q
 JOIN photo.runs r ON r.tenant_id=q.tenant_id AND r.id=q.run_id
 WHERE q.state IN ('blocked','ready','queued','retry_wait')
 AND (q.cancel_requested OR r.cancel_requested)
 ORDER BY q.created_at,q.id LIMIT 1;
 IF candidate IS NULL THEN RETURN 0; END IF;
 t:=photo.lock_task(candidate);
 IF t.id IS NULL OR t.state NOT IN ('blocked','ready','queued','retry_wait') THEN RETURN 0; END IF;
 IF NOT (t.cancel_requested OR EXISTS(SELECT FROM photo.runs r
  WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.cancel_requested)) THEN RETURN 0; END IF;
 PERFORM photo.release_task_resources(t.tenant_id,t.id,t.fencing_token);
 UPDATE photo.tasks SET state='cancelled',cancel_requested=true,
  fencing_token=fencing_token+1,dispatch_command=NULL,next_attempt_at=NULL,
  lease_until=NULL,terminal_reason='cancel_requested' WHERE id=t.id AND tenant_id=t.tenant_id;
 RETURN 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.cancel_idle_task() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_idle_task() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.cancel_idle_task() TO photo_scheduler,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
