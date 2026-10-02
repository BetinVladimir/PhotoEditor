-- +goose Up
-- Mutate at most one task per transaction. The Go service repeats short calls;
-- retaining one run lock while acquiring another task slot would invert locks.
-- +goose StatementBegin
CREATE FUNCTION photo.reap_tasks(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid; t photo.tasks%ROWTYPE; err jsonb; ev uuid; cancelled boolean; count_rows integer:=0;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'INVALID_LIMIT' USING ERRCODE='22023'; END IF;
 FOR candidate IN SELECT q.id FROM photo.tasks q WHERE
 (q.state='running' AND (q.lease_until<=clock_timestamp() OR q.deadline<=clock_timestamp())) OR
 (q.state='queued' AND (q.cancel_requested OR NOT EXISTS(SELECT FROM photo.resource_reservations r WHERE r.tenant_id=q.tenant_id AND r.task_id=q.id AND r.state='reserved' AND r.expires_at>clock_timestamp())))
 ORDER BY q.created_at,q.id LIMIT 1 LOOP
  t:=photo.lock_task(candidate);
  IF t.state NOT IN ('queued','running') THEN CONTINUE; END IF;
  SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;
  IF t.state='running' AND t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() THEN CONTINUE; END IF;
  IF t.state='queued' AND NOT cancelled AND EXISTS(SELECT FROM photo.resource_reservations r WHERE r.tenant_id=t.tenant_id AND r.task_id=t.id AND r.state='reserved' AND r.expires_at>clock_timestamp()) THEN CONTINUE; END IF;
  count_rows:=count_rows+1;
  PERFORM photo.release_task_resources(t.tenant_id,t.id,t.fencing_token);
  IF t.state='queued' THEN
   UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE 'ready' END,dispatch_command=NULL,
    fencing_token=fencing_token+1,terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE 'dispatch_lease_expired' END WHERE id=t.id;
   CONTINUE;
  END IF;
  err:=jsonb_build_object('code',CASE WHEN t.deadline<=clock_timestamp() THEN 'TIMEOUT' ELSE 'LEASE_LOST' END,
  'retryable',true,'message_key',CASE WHEN t.deadline<=clock_timestamp() THEN 'errors.timeout' ELSE 'errors.lease_lost' END,'args','{}'::jsonb,'diagnostic_artifact_id',NULL);
  UPDATE photo.task_attempts SET finished_at=clock_timestamp(),error=err WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=t.attempt AND finished_at IS NULL;
  UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE 'retry_wait' END,lease_until=NULL,next_attempt_at=NULL,
   fencing_token=fencing_token+1,terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE err->>'code' END WHERE id=t.id;
  ev:=gen_random_uuid();
  INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,ev,'photo.events.v1',CASE WHEN cancelled THEN 'task.cancelled' ELSE 'task.failed' END,
   jsonb_build_object('schema_version',1,'event_id',ev,'type',CASE WHEN cancelled THEN 'task.cancelled' ELSE 'task.failed' END,
   'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,'attempt',t.attempt,'fencing_token',t.fencing_token,
   'outputs','[]'::jsonb,'metrics',jsonb_build_object('duration_ms',greatest(0,extract(epoch FROM(clock_timestamp()-(SELECT started_at FROM photo.task_attempts WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=t.attempt)))*1000)::bigint,'peak_memory_bytes',0),'error',err));
 END LOOP; RETURN count_rows;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reap_tasks(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reap_tasks(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reap_tasks(integer) TO photo_scheduler,photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.pending_task_failures(p_limit integer)
RETURNS TABLE(task_id uuid,attempt integer,fence bigint,retry_count integer,oom_count integer,cancel_requested boolean,error jsonb,profile jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'INVALID_LIMIT' USING ERRCODE='22023'; END IF;
 RETURN QUERY SELECT t.id,t.attempt,t.fencing_token,t.retry_count,t.oom_count,t.cancel_requested OR r.cancel_requested,a.error,t.execution_profile
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.task_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.id AND a.attempt=t.attempt
 WHERE t.state='retry_wait' AND t.next_attempt_at IS NULL AND a.error IS NOT NULL ORDER BY a.finished_at,t.id LIMIT p_limit;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_task_failures(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_task_failures(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pending_task_failures(integer) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.plan_task_retry(p_task uuid,p_attempt integer,p_fence bigint,p_state text,p_retry integer,p_oom integer,p_next timestamptz,p_profile jsonb,p_reason text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; cancelled boolean;
BEGIN
 IF p_state IS NULL OR p_state NOT IN ('retry_wait','failed','cancelled') OR p_retry IS NULL OR p_retry NOT BETWEEN 0 AND 3 OR p_oom IS NULL OR p_oom NOT BETWEEN 0 AND 2 OR p_profile IS NULL OR jsonb_typeof(p_profile)<>'object'
 OR p_reason IS NULL OR length(p_reason)>120 OR (p_state='retry_wait' AND (p_next IS NULL OR p_next>clock_timestamp()+interval '150 seconds'))
 THEN RAISE EXCEPTION 'INVALID_RETRY' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR t.state<>'retry_wait' OR t.next_attempt_at IS NOT NULL OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence THEN RETURN false; END IF;
 IF p_retry<t.retry_count OR p_retry>t.retry_count+1 OR p_oom<t.oom_count OR p_oom>t.oom_count+1 OR
 (p_state='retry_wait' AND (t.attempt>=6 OR p_retry+p_oom<>t.retry_count+t.oom_count+1)) THEN RAISE EXCEPTION 'RETRY_BUDGET' USING ERRCODE='22023'; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;
 UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE p_state END,retry_count=p_retry,oom_count=p_oom,
  next_attempt_at=CASE WHEN NOT cancelled AND p_state='retry_wait' THEN p_next ELSE NULL END,execution_profile=p_profile,
  terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE p_reason END WHERE id=t.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.plan_task_retry(uuid,integer,bigint,text,integer,integer,timestamptz,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.plan_task_retry(uuid,integer,bigint,text,integer,integer,timestamptz,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.plan_task_retry(uuid,integer,bigint,text,integer,integer,timestamptz,jsonb,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.activate_task_retries(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid; t photo.tasks%ROWTYPE; cancelled boolean; count_rows integer:=0;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'INVALID_LIMIT' USING ERRCODE='22023'; END IF;
 FOR candidate IN SELECT id FROM photo.tasks WHERE state='retry_wait' AND next_attempt_at<=clock_timestamp() ORDER BY next_attempt_at,id LIMIT 1 LOOP
  t:=photo.lock_task(candidate);
  IF t.state<>'retry_wait' OR t.next_attempt_at IS NULL OR t.next_attempt_at>clock_timestamp() THEN CONTINUE; END IF;
  SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;
  UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE 'ready' END,next_attempt_at=NULL,dispatch_command=NULL,
   terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE NULL END WHERE id=t.id;
  count_rows:=count_rows+1;
 END LOOP; RETURN count_rows;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.activate_task_retries(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_task_retries(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_task_retries(integer) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
