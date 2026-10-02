-- +goose Up
-- Worker logins are provisioned by the operator; a worker cannot self-authorize.
ALTER TABLE photo.worker_registrations ADD COLUMN qualified_operations jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(qualified_operations)='object');
ALTER TABLE photo.worker_registrations ADD COLUMN db_principal name;
ALTER TABLE photo.worker_registrations ADD COLUMN node_id text;
ALTER TABLE photo.worker_registrations ADD COLUMN enabled boolean NOT NULL DEFAULT false;
ALTER TABLE photo.tasks ADD COLUMN dispatch_command jsonb;
ALTER TABLE photo.task_attempts ADD COLUMN metrics jsonb;
ALTER TABLE photo.tasks ADD COLUMN progress jsonb NOT NULL DEFAULT '{}';
ALTER TABLE photo.tasks ADD COLUMN timeout_seconds integer NOT NULL DEFAULT 600 CHECK(timeout_seconds BETWEEN 1 AND 7200);
ALTER TABLE photo.resource_reservations ADD COLUMN fencing_token bigint NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX resource_slot_one_task ON photo.resource_slots(task_id) WHERE task_id IS NOT NULL;
ALTER TABLE photo.artifacts ADD COLUMN produced_attempt integer;
ALTER TABLE photo.artifacts ADD COLUMN produced_fence bigint;
ALTER TABLE photo.artifacts ADD COLUMN verified_at timestamptz;
ALTER TABLE photo.artifacts ADD COLUMN output_role text;
CREATE INDEX task_reaper ON photo.tasks(lease_until,id) WHERE state='running';

-- Private helper: every mutation follows slots -> usage -> run -> task.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_task(p_task uuid) RETURNS photo.tasks
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; locked photo.tasks%ROWTYPE;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN NULL; END IF;
 PERFORM id FROM photo.resource_slots WHERE task_id=t.id ORDER BY node_id,slot_name FOR UPDATE;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=t.tenant_id FOR UPDATE;
 PERFORM id FROM photo.runs WHERE id=t.run_id AND tenant_id=t.tenant_id FOR UPDATE;
 SELECT * INTO locked FROM photo.tasks WHERE id=t.id AND tenant_id=t.tenant_id FOR UPDATE;
 RETURN locked;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_task(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_task(uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.worker_authorized(p_tenant uuid,p_worker uuid,p_class text,p_node text,p_operation text,p_backend text,p_lock_id text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=p_tenant AND w.worker_id=p_worker
 AND w.db_principal=session_user AND w.enabled AND w.node_id=p_node AND w.resource_classes ? p_class
 AND (w.qualified_operations->p_operation) ? p_backend AND w.backends ? p_backend AND (p_lock_id IS NULL OR w.lock_id=p_lock_id));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.worker_authorized(uuid,uuid,text,text,text,text,text) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_task(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; slot photo.resource_slots%ROWTYPE; r photo.runs%ROWTYPE; until_time timestamptz; end_time timestamptz;
BEGIN
 IF p_attempt IS NULL OR p_attempt NOT BETWEEN 1 AND 6 OR p_worker IS NULL OR p_command IS NULL OR octet_length(p_command::text)>65536 THEN
  RAISE EXCEPTION 'INVALID_COMMAND' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL THEN RETURN QUERY SELECT 'missing',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=t.id AND tenant_id=t.tenant_id;
 -- Terminal duplicates still require a registered identity, but no longer own a slot.
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker
    AND w.db_principal=session_user AND w.enabled AND w.resource_classes ? t.resource_class) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.state IN ('succeeded','failed','cancelled','skipped') THEN
  RETURN QUERY SELECT 'terminal',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.dispatch_command IS NULL OR t.dispatch_command<>p_command THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE id=t.run_id AND tenant_id=t.tenant_id;
 IF t.cancel_requested OR r.cancel_requested THEN
  RETURN QUERY SELECT 'cancelled',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.state='running' THEN RETURN QUERY SELECT 'busy',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN; END IF;
 IF t.state<>'queued' OR p_attempt<>t.attempt+1 OR (p_command->>'attempt')::integer<>p_attempt THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF slot.id IS NULL OR slot.lease_until IS NULL OR slot.fencing_token IS DISTINCT FROM t.fencing_token OR slot.lease_until<=clock_timestamp() OR
 NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') OR
 NOT EXISTS(SELECT FROM photo.resource_reservations rr WHERE rr.tenant_id=t.tenant_id AND rr.task_id=t.id AND rr.node_id=slot.node_id
 AND rr.state='reserved' AND rr.expires_at>clock_timestamp()) THEN
  RETURN QUERY SELECT 'unavailable',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 end_time:=clock_timestamp()+make_interval(secs=>t.timeout_seconds);
 until_time:=least(clock_timestamp()+interval '60 seconds',end_time);
 UPDATE photo.tasks SET state='running',attempt=p_attempt,fencing_token=t.fencing_token+1,worker_id=p_worker,
 lease_until=until_time,deadline=end_time,progress='{}' WHERE id=t.id;
 UPDATE photo.resource_slots SET fencing_token=t.fencing_token+1,lease_until=until_time WHERE id=slot.id;
 UPDATE photo.resource_reservations SET fencing_token=t.fencing_token+1,expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile)
 VALUES(t.tenant_id,t.id,p_attempt,t.fencing_token+1,p_worker,clock_timestamp(),t.execution_profile->>'backend',t.execution_profile);
 RETURN QUERY SELECT 'claimed',t.fencing_token+1,until_time,end_time,
 jsonb_build_object('command',t.dispatch_command,'execution_profile',t.execution_profile,'input_digest',t.input_digest);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_task(uuid,integer,uuid,jsonb) TO photo_worker;

-- +goose StatementBegin
CREATE FUNCTION photo.heartbeat_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_progress jsonb)
RETURNS TABLE(accepted boolean,lease_until timestamptz,cancel_requested boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; until_time timestamptz; cancelled boolean; slot photo.resource_slots%ROWTYPE;
BEGIN
 IF p_progress IS NULL OR jsonb_typeof(p_progress)<>'object' OR octet_length(p_progress::text)>4096 THEN
 RAISE EXCEPTION 'INVALID_PROGRESS' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=p_task AND tenant_id=t.tenant_id;
 IF t.id IS NULL OR t.state<>'running' OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.lease_until<=clock_timestamp() OR (t.deadline IS NULL OR t.deadline<=clock_timestamp())
 OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.id IS NULL
 OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false; RETURN; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;
 until_time:=least(clock_timestamp()+interval '60 seconds',t.deadline);
 UPDATE photo.tasks SET lease_until=until_time,progress=p_progress WHERE id=t.id;
 UPDATE photo.resource_slots SET lease_until=until_time WHERE id=slot.id AND fencing_token=p_fence;
 UPDATE photo.resource_reservations SET expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id AND fencing_token=p_fence;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 RETURN QUERY SELECT true,until_time,cancelled;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) TO photo_worker;

-- Private helper called only with task/slot locks and a validated fence.
-- +goose StatementBegin
CREATE FUNCTION photo.release_task_resources(p_tenant uuid,p_task uuid,p_fence bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$ BEGIN
 UPDATE photo.resource_slots SET tenant_id=NULL,task_id=NULL,fencing_token=NULL,lease_until=NULL
 WHERE tenant_id=p_tenant AND task_id=p_task AND fencing_token=p_fence;
 UPDATE photo.resource_reservations SET state='released',expires_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND task_id=p_task AND fencing_token=p_fence;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_task_resources(uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_task_resources(uuid,uuid,bigint) FROM PUBLIC;

-- Canonical manifest bytes are prepared by the shared RFC8785 encoder. The SQL
-- boundary computes their digest and additionally compares the full JSON on replay.
-- Artifacts may only be attested by the trusted object gateway, never by workers.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_task_success(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_manifest text)
RETURNS TABLE(outcome text,result_id uuid,result_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; slot photo.resource_slots%ROWTYPE; m jsonb; old photo.task_results%ROWTYPE;
 a jsonb; stored photo.artifacts%ROWTYPE; result uuid; digest text; ev uuid:=gen_random_uuid();
BEGIN
 IF p_manifest IS NULL OR octet_length(p_manifest)>49152 THEN RAISE EXCEPTION 'INVALID_RESULT' USING ERRCODE='22023'; END IF;
 m:=p_manifest::jsonb;
 IF jsonb_typeof(m)<>'object' OR jsonb_typeof(m->'outputs') IS DISTINCT FROM 'array' OR jsonb_array_length(m->'outputs')>128
 OR jsonb_typeof(m->'metrics') IS DISTINCT FROM 'object' OR jsonb_typeof(m->'review_required') IS DISTINCT FROM 'boolean'
 OR (m-'outputs'-'metrics'-'review_required')<>'{}' THEN RAISE EXCEPTION 'INVALID_RESULT' USING ERRCODE='22023'; END IF;
 digest:=encode(sha256(convert_to(p_manifest,'UTF8')),'hex');
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR t.worker_id IS DISTINCT FROM p_worker OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled)
 THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text; RETURN; END IF;
 IF t.state='succeeded' THEN
  SELECT * INTO old FROM photo.task_results WHERE tenant_id=t.tenant_id AND id=t.canonical_result_id;
  IF old.result_digest=digest AND old.outputs=m->'outputs' AND old.metrics=m->'metrics' AND old.review_required=(m->>'review_required')::boolean THEN
   RETURN QUERY SELECT 'replayed',old.id,old.result_digest;
  ELSE RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::text; END IF; RETURN;
 END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=t.id AND tenant_id=t.tenant_id;
 IF t.state<>'running' OR t.lease_until<=clock_timestamp() OR (t.deadline IS NULL OR t.deadline<=clock_timestamp()) OR slot.id IS NULL
 OR slot.fencing_token IS DISTINCT FROM p_fence OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
 RETURN QUERY SELECT 'stale',NULL::uuid,NULL::text; RETURN; END IF;
 IF t.cancel_requested OR EXISTS(SELECT FROM photo.runs WHERE id=t.run_id AND cancel_requested) THEN
 RETURN QUERY SELECT 'cancelled',NULL::uuid,NULL::text; RETURN; END IF;
 IF (SELECT count(*)<>count(DISTINCT x->>'artifact_id') FROM jsonb_array_elements(m->'outputs') x) THEN
 RAISE EXCEPTION 'DUPLICATE_OUTPUT' USING ERRCODE='22023'; END IF;
 FOR a IN SELECT value FROM jsonb_array_elements(m->'outputs') ORDER BY value->>'artifact_id' LOOP
  IF jsonb_typeof(a)<>'object' OR (a-'artifact_id'-'sha256')<>'{}' OR a->>'sha256' IS NULL OR a->>'artifact_id' IS NULL THEN
   RAISE EXCEPTION 'INVALID_OUTPUT' USING ERRCODE='22023'; END IF;
  SELECT * INTO stored FROM photo.artifacts WHERE tenant_id=t.tenant_id AND id=(a->>'artifact_id')::uuid FOR UPDATE;
  IF NOT FOUND OR stored.sha256<>a->>'sha256' OR stored.state<>'ready' OR stored.verified_at IS NULL OR
   stored.created_by_task_id IS DISTINCT FROM t.id OR stored.produced_attempt IS DISTINCT FROM p_attempt OR stored.produced_fence IS DISTINCT FROM p_fence
   OR (stored.expires_at IS NOT NULL AND stored.expires_at<=clock_timestamp()) THEN
   RAISE EXCEPTION 'OUTPUT_NOT_VERIFIED' USING ERRCODE='22023'; END IF;
 END LOOP;
 INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)
 VALUES(t.tenant_id,t.id,p_attempt,p_fence,m->'outputs',m->'metrics',(m->>'review_required')::boolean,digest) RETURNING id INTO result;
 UPDATE photo.tasks SET state='succeeded',canonical_result_id=result,lease_until=NULL,terminal_reason=NULL WHERE id=t.id;
 UPDATE photo.task_attempts SET finished_at=clock_timestamp(),metrics=m->'metrics' WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=p_attempt;
 PERFORM photo.release_task_resources(t.tenant_id,t.id,p_fence);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,ev,'photo.events.v1','task.succeeded',
 jsonb_build_object('schema_version',1,'event_id',ev,'type','task.succeeded','tenant_id',t.tenant_id,'run_id',t.run_id,
 'task_id',t.id,'attempt',p_attempt,'fencing_token',p_fence,'outputs',m->'outputs','metrics',m->'metrics','error',NULL));
 RETURN QUERY SELECT 'committed',result,digest;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text) TO photo_worker;

-- Failure is durably recorded as retry_wait without a scheduled date. Only the
-- Go scheduler chooses retries and sets next_attempt_at, or makes it terminal.
-- +goose StatementBegin
CREATE FUNCTION photo.commit_task_failure(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_error jsonb,p_metrics jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; slot photo.resource_slots%ROWTYPE; ev uuid:=gen_random_uuid(); cancelled boolean; old_error jsonb; old_metrics jsonb;
BEGIN
 IF p_error IS NULL OR jsonb_typeof(p_error)<>'object' OR jsonb_typeof(p_error->'code') IS DISTINCT FROM 'string'
 OR jsonb_typeof(p_error->'retryable') IS DISTINCT FROM 'boolean' OR octet_length(p_error::text)>8192
 OR p_metrics IS NULL OR jsonb_typeof(p_metrics)<>'object' OR octet_length(p_metrics::text)>4096 THEN
 RAISE EXCEPTION 'INVALID_FAILURE' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
 OR NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled)
 THEN RETURN 'stale'; END IF;
 IF t.state IN ('retry_wait','failed','cancelled') THEN
 SELECT error,metrics INTO old_error,old_metrics FROM photo.task_attempts WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=p_attempt;
 IF old_error=p_error AND old_metrics=p_metrics THEN RETURN 'replayed'; ELSE RETURN 'conflict'; END IF;
 END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=t.id AND tenant_id=t.tenant_id;
 IF t.state<>'running' OR t.lease_until<=clock_timestamp() OR (t.deadline IS NULL OR t.deadline<=clock_timestamp()) OR slot.id IS NULL
 OR slot.fencing_token IS DISTINCT FROM p_fence OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN RETURN 'stale'; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;
 UPDATE photo.tasks SET state=CASE WHEN cancelled THEN 'cancelled' ELSE 'retry_wait' END,lease_until=NULL,next_attempt_at=NULL,
 terminal_reason=CASE WHEN cancelled THEN 'cancel_requested' ELSE p_error->>'code' END WHERE id=t.id;
 UPDATE photo.task_attempts SET finished_at=clock_timestamp(),error=p_error,metrics=p_metrics WHERE tenant_id=t.tenant_id AND task_id=t.id AND attempt=p_attempt;
 PERFORM photo.release_task_resources(t.tenant_id,t.id,p_fence);
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,ev,'photo.events.v1',CASE WHEN cancelled THEN 'task.cancelled' ELSE 'task.failed' END,
 jsonb_build_object('schema_version',1,'event_id',ev,'type',CASE WHEN cancelled THEN 'task.cancelled' ELSE 'task.failed' END,
 'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,'attempt',p_attempt,'fencing_token',p_fence,'outputs','[]'::jsonb,'metrics',p_metrics,'error',p_error));
 RETURN 'committed';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
