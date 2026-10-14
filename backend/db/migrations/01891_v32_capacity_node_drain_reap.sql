-- +goose Up
-- Reap only an expired task still occupying the pinned draining node. Healthy
-- leases are never fenced merely because an operator's wait timed out.
-- +goose StatementBegin
CREATE FUNCTION photo.reap_capacity_node_drain_task(p_node text,p_generation bigint)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE n photo.resource_nodes%ROWTYPE; candidate uuid; t photo.tasks%ROWTYPE;
 cancelled boolean; failure jsonb; ev uuid;
BEGIN
 IF p_node IS NULL OR p_generation IS NULL OR p_generation<1 THEN
  RAISE EXCEPTION 'INVALID_DRAIN_PROOF' USING ERRCODE='22023';
 END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE;
 IF NOT FOUND OR n.enabled OR n.drain_started_at IS NULL OR n.drain_generation<>p_generation THEN
  RAISE EXCEPTION 'STALE_DRAIN_PROOF' USING ERRCODE='22023';
 END IF;
 SELECT s.task_id INTO candidate FROM photo.resource_slots s
 JOIN photo.tasks q ON q.id=s.task_id AND q.tenant_id=s.tenant_id
 WHERE s.node_id=p_node AND (
  (q.state='running' AND (q.lease_until<=clock_timestamp() OR q.deadline<=clock_timestamp())) OR
  (q.state='queued' AND (q.cancel_requested OR NOT EXISTS(
   SELECT 1 FROM photo.resource_reservations r WHERE r.tenant_id=q.tenant_id AND r.task_id=q.id
    AND r.state='reserved' AND r.expires_at>clock_timestamp()))))
 ORDER BY q.created_at,q.id LIMIT 1;
 IF candidate IS NULL THEN RETURN 0; END IF;
 t:=photo.lock_task(candidate);
 IF t.id IS NULL OR t.state NOT IN('queued','running') OR NOT EXISTS(
  SELECT 1 FROM photo.resource_slots s WHERE s.node_id=p_node AND s.task_id=t.id AND s.tenant_id=t.tenant_id)
 THEN RETURN 0; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r
  WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;
 IF t.state='running' AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() THEN RETURN 0; END IF;
 IF t.state='queued' AND NOT cancelled AND EXISTS(
  SELECT 1 FROM photo.resource_reservations r WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id
   AND r.state='reserved' AND r.expires_at>clock_timestamp()) THEN RETURN 0; END IF;
 PERFORM photo.release_task_resources(t.tenant_id,t.id,t.fencing_token);
 IF t.state='queued' THEN
  UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE 'ready' END,
   dispatch_command=NULL,fencing_token=fencing_token+1,
   terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE 'dispatch_lease_expired' END
   WHERE id=t.id;
  RETURN 1;
 END IF;
 failure:=jsonb_build_object('code',CASE WHEN t.deadline<=clock_timestamp() THEN 'TIMEOUT' ELSE 'LEASE_LOST' END,
  'retryable',true,'message_key',CASE WHEN t.deadline<=clock_timestamp() THEN 'errors.timeout' ELSE 'errors.lease_lost' END,
  'args','{}'::jsonb,'diagnostic_artifact_id',NULL);
 UPDATE photo.task_attempts SET finished_at=clock_timestamp(),error=failure
  WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=t.attempt AND finished_at IS NULL;
 UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE 'retry_wait' END,
  lease_until=NULL,next_attempt_at=NULL,fencing_token=fencing_token+1,
  terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE failure->>'code' END WHERE id=t.id;
 ev:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(t.tenant_id,ev,'photo.events.v1',CASE WHEN cancelled THEN 'task.cancelled' ELSE 'task.failed' END,
  jsonb_build_object('schema_version',1,'event_id',ev,
   'type',CASE WHEN cancelled THEN 'task.cancelled' ELSE 'task.failed' END,
   'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,'attempt',t.attempt,
   'fencing_token',t.fencing_token,'outputs','[]'::jsonb,
   'metrics',jsonb_build_object('duration_ms',greatest(0,extract(epoch FROM
    (clock_timestamp()-(SELECT started_at FROM photo.task_attempts
     WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=t.attempt)))*1000)::bigint,
    'peak_memory_bytes',0),'error',failure));
 RETURN 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reap_capacity_node_drain_task(text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reap_capacity_node_drain_task(text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reap_capacity_node_drain_task(text,bigint) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
