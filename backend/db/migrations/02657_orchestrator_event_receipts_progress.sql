-- STAGED CANONICAL2657 PROPOSAL. ROOT SOURCE REVIEW AND INDEPENDENT CLUSTER QUALIFICATION REQUIRED.
-- UNNUMBERED SOURCE DRAFT. Not approved for installation. Actual source predecessor head2656.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
 IF (SELECT max(version_id)FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 2656 THEN RAISE EXCEPTION 'EVENT_HEAD_PREIMAGE';END IF;
 IF EXISTS(SELECT FROM pg_roles WHERE rolname='photo_event_consumer') OR to_regprocedure('photo.accept_orchestrator_event(uuid,uuid,text,jsonb)') IS NOT NULL THEN RAISE EXCEPTION 'EVENT_ALREADY_PRESENT';END IF;
 IF EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.inbox'::regclass AND attname='payload_sha256'AND NOT attisdropped)THEN RAISE EXCEPTION 'EVENT_INBOX_PREIMAGE';END IF;
END $$;
-- +goose StatementEnd
-- Actual full current catalog/ACL/default privilege preimages MUST be captured before promotion.
CREATE ROLE photo_event_consumer NOLOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION;
GRANT USAGE ON SCHEMA photo TO photo_event_consumer;
ALTER TABLE photo.inbox ADD COLUMN payload_sha256 text CHECK(payload_sha256 IS NULL OR payload_sha256 ~ '^[0-9a-f]{64}$');
-- +goose StatementBegin
CREATE FUNCTION photo.accept_orchestrator_event(p_tenant uuid,p_event uuid,p_routing text,p_payload jsonb)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
-- event-consumer-v1: receipt/UI only; existing reconciliation advances domain state.
DECLARE o photo.outbox%ROWTYPE;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;
 prior text;hash text;fresh uuid;cursor_value bigint;body jsonb;projection text;review_row photo.reviews%ROWTYPE;delivery_row photo.delivery_packages%ROWTYPE;is_stale boolean:=false;
BEGIN
 IF p_tenant IS NULL OR p_event IS NULL OR p_routing IS NULL OR p_payload IS NULL OR jsonb_typeof(p_payload)<>'object'
 OR jsonb_typeof(p_payload->'schema_version') IS DISTINCT FROM 'number' OR p_payload->>'schema_version' IS DISTINCT FROM '1'
 OR octet_length(p_payload::text)>65536 OR p_routing NOT IN('task.succeeded','task.failed','task.cancelled','task.progress','download_status','review.created','delivery.cancelled') THEN RETURN 'rejected';END IF;
 IF p_routing LIKE 'task.%' AND (
  jsonb_typeof(p_payload->'outputs') IS DISTINCT FROM 'array' OR jsonb_typeof(p_payload->'metrics') IS DISTINCT FROM 'object'
  OR jsonb_typeof(p_payload->'attempt') IS DISTINCT FROM 'number' OR coalesce(p_payload->>'attempt','') !~ '^[1-9][0-9]{0,8}$'
  OR jsonb_typeof(p_payload->'fencing_token') IS DISTINCT FROM 'number' OR coalesce(p_payload->>'fencing_token','') !~ '^[1-9][0-9]{0,15}$'
  OR (p_routing<>'task.succeeded' AND p_payload->'outputs'<>'[]'::jsonb)
  OR (p_routing IN('task.succeeded','task.progress') AND p_payload->'error' IS DISTINCT FROM 'null'::jsonb)
 ) THEN RETURN 'rejected';END IF;
 IF p_routing='task.progress' AND (
  jsonb_typeof(p_payload->'metrics'->'duration_ms') IS DISTINCT FROM 'number' OR coalesce(p_payload->'metrics'->>'duration_ms','') !~ '^(0|[1-9][0-9]{0,15})$'
  OR jsonb_typeof(p_payload->'metrics'->'peak_memory_bytes') IS DISTINCT FROM 'number' OR coalesce(p_payload->'metrics'->>'peak_memory_bytes','') !~ '^(0|[1-9][0-9]{0,15})$'
  OR jsonb_typeof(p_payload->'metrics'->'done') IS DISTINCT FROM 'number' OR coalesce(p_payload->'metrics'->>'done','') !~ '^(0|[1-9][0-9]{0,15})$'
  OR jsonb_typeof(p_payload->'metrics'->'total') IS DISTINCT FROM 'number' OR coalesce(p_payload->'metrics'->>'total','') !~ '^(0|[1-9][0-9]{0,15})$'
 ) THEN RETURN 'rejected';END IF;
 IF p_routing='task.progress' AND ((p_payload->'metrics'->>'done')::bigint>(p_payload->'metrics'->>'total')::bigint
  OR (p_payload->'metrics'->>'total')::bigint>9007199254740991
  OR (p_payload->'metrics'->>'duration_ms')::bigint>9007199254740991 OR (p_payload->'metrics'->>'peak_memory_bytes')::bigint>9007199254740991) THEN RETURN 'rejected';END IF;
 SELECT * INTO o FROM photo.outbox WHERE tenant_id=p_tenant AND event_id=p_event;
 IF NOT FOUND OR o.destination<>'photo.events.v1' OR o.routing_key<>p_routing OR o.payload IS DISTINCT FROM p_payload
 OR p_payload->>'event_id' IS DISTINCT FROM p_event::text OR p_payload->>'tenant_id' IS DISTINCT FROM p_tenant::text
 OR p_payload->>'type' IS DISTINCT FROM p_routing THEN RETURN 'rejected';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=(p_payload->>'run_id')::uuid;
 IF NOT FOUND THEN RETURN 'rejected';END IF;
 IF p_routing LIKE 'task.%' THEN
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id=(p_payload->>'task_id')::uuid;
  IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.task_attempts a WHERE a.tenant_id=p_tenant AND a.task_id=t.id
   AND a.attempt=(p_payload->>'attempt')::int AND a.fencing_token=(p_payload->>'fencing_token')::bigint AND (p_routing='task.progress' OR a.finished_at IS NOT NULL)) THEN
   RETURN 'rejected';END IF;
  IF t.attempt IS DISTINCT FROM (p_payload->>'attempt')::int OR t.fencing_token IS DISTINCT FROM (p_payload->>'fencing_token')::bigint THEN is_stale:=true;END IF;
  IF NOT is_stale AND p_routing='task.succeeded' AND (t.state IS DISTINCT FROM 'succeeded' OR NOT EXISTS(SELECT FROM photo.task_results x WHERE x.tenant_id=p_tenant AND x.task_id=t.id
   AND x.attempt=t.attempt AND x.fencing_token=t.fencing_token AND x.id=t.canonical_result_id AND x.outputs IS NOT DISTINCT FROM p_payload->'outputs')) THEN
   RETURN 'rejected';END IF;
  IF p_routing='task.progress' AND (t.state IS DISTINCT FROM 'running' OR t.cancel_requested OR r.cancel_requested
   OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()) THEN is_stale:=true;END IF;
  -- Private invalidation only: UI reloads GET; progress never publishes completion or changes graph.
  projection:=CASE WHEN p_routing='task.progress' THEN 'task_progress' ELSE 'task_status' END;
  body:=jsonb_build_object('run_id',r.id,'task_id',t.id);
 ELSIF p_routing='delivery.cancelled' THEN
  SELECT p.* INTO delivery_row FROM photo.delivery_packages p JOIN photo.v2_workflows w ON w.tenant_id=p.tenant_id AND w.id=p.workflow_id
  WHERE p.tenant_id=p_tenant AND p.id=(p_payload->>'package_id')::uuid AND w.run_id=r.id AND w.workflow_type='delivery';
  IF NOT FOUND THEN RETURN 'rejected';END IF;
  IF delivery_row.state<>'cancelled' THEN is_stale:=true;END IF;
  projection:='delivery.cancelled';body:=jsonb_build_object('run_id',r.id,'package_id',delivery_row.id,'status','cancelled');
 ELSIF p_routing='review.created' THEN
  SELECT * INTO review_row FROM photo.reviews WHERE tenant_id=p_tenant AND run_id=r.id AND id=(p_payload->>'review_id')::uuid;
  IF NOT FOUND OR review_row.kind IS DISTINCT FROM p_payload->>'kind' THEN RETURN 'rejected';END IF;
  projection:='review.created';body:=jsonb_build_object('run_id',r.id,'review_id',review_row.id,'kind',review_row.kind);
 ELSE
  -- download_status producer already wrote principal run_events in the same commit.
  projection:=NULL;
 END IF;
 hash:=encode(sha256(convert_to(p_payload::text,'UTF8')),'hex');
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at,payload_sha256)
 VALUES(p_tenant,'orchestrator-events-v1',p_event,clock_timestamp(),hash)
 ON CONFLICT(tenant_id,consumer,event_id) DO NOTHING RETURNING id INTO fresh;
 IF fresh IS NULL THEN
  SELECT payload_sha256 INTO prior FROM photo.inbox WHERE tenant_id=p_tenant AND consumer='orchestrator-events-v1' AND event_id=p_event;
  IF prior IS DISTINCT FROM hash THEN RETURN 'rejected';END IF;
  RETURN 'duplicate';
 END IF;
 IF is_stale THEN RETURN 'stale';END IF;
 IF projection IS NULL THEN RETURN 'accepted';END IF;
 IF r.created_by IS NULL THEN RETURN 'accepted';END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,cursor_value,r.id,r.created_by,'principal',projection,body);
 RETURN 'accepted';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.accept_orchestrator_event(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.accept_orchestrator_event(uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.accept_orchestrator_event(uuid,uuid,text,jsonb) TO photo_event_consumer;
-- Event-only DLQ metadata, never a command replay capability.
ALTER TABLE photo.dead_letters DROP CONSTRAINT dead_letters_worker_class_check;
ALTER TABLE photo.dead_letters ADD CONSTRAINT dead_letters_worker_class_check CHECK(worker_class IN('cpu','gpu','media','archive','collection','mail','unknown','events'));
CREATE UNIQUE INDEX dead_letters_events_digest ON photo.dead_letters(routing_key,payload_sha256) WHERE worker_class='events';
-- +goose StatementBegin
CREATE FUNCTION photo.record_event_dead_letter(p_routing text,p_sha text,p_metadata jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE found uuid;
BEGIN
 IF p_routing IS NULL OR p_sha IS NULL OR p_metadata IS NULL OR p_routing<>'dead.events' OR p_sha !~ '^[0-9a-f]{64}$' OR p_metadata IS DISTINCT FROM '{"schema_version":1,"parse_status":"event_quarantine"}'::jsonb THEN RETURN NULL;END IF;
 INSERT INTO photo.dead_letters(worker_class,routing_key,payload_sha256,metadata)
 VALUES('events',p_routing,p_sha,p_metadata)
 ON CONFLICT(routing_key,payload_sha256)WHERE worker_class='events' DO UPDATE SET seen_count=photo.dead_letters.seen_count+1,last_seen_at=clock_timestamp() RETURNING id INTO found;
 RETURN found;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.record_event_dead_letter(text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_event_dead_letter(text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_event_dead_letter(text,text,jsonb) TO photo_publisher;
-- Staged2657 progress producer; no new direct worker capability.
-- Invoked only by successful existing heartbeat, with its run/task/slot locks held.
CREATE INDEX task_progress_outbox_lookup ON photo.outbox(tenant_id,(payload->>'task_id'),created_at DESC,id DESC)
 WHERE destination='photo.events.v1' AND routing_key='task.progress';
-- +goose StatementBegin
CREATE FUNCTION photo.emit_task_progress(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;s photo.resource_slots%ROWTYPE;r photo.runs%ROWTYPE;
 prev photo.outbox%ROWTYPE;d bigint;n bigint;ev uuid;stamp timestamptz:=clock_timestamp();started timestamptz;
BEGIN
 -- Private function: no worker/API/consumer EXECUTE. Existing heartbeat owns lock ordering.
 SELECT * INTO t FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND OR t.state IS DISTINCT FROM 'running' OR t.attempt IS DISTINCT FROM p_attempt
 OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
 OR t.lease_until IS NULL OR t.lease_until<=stamp OR t.deadline IS NULL OR t.deadline<=stamp OR t.cancel_requested THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 IF NOT FOUND OR r.cancel_requested THEN RETURN;END IF;
 SELECT * INTO s FROM photo.resource_slots WHERE tenant_id=t.tenant_id AND task_id=t.id;
 IF NOT FOUND OR s.fencing_token IS DISTINCT FROM p_fence OR s.lease_until IS NULL OR s.lease_until<=stamp
 OR photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,s.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') IS NOT TRUE THEN RETURN;END IF;
 IF jsonb_typeof(t.progress) IS DISTINCT FROM 'object'
 OR jsonb_typeof(t.progress->'done') IS DISTINCT FROM 'number' OR jsonb_typeof(t.progress->'total') IS DISTINCT FROM 'number'
 OR (t.progress->>'done') !~ '^(0|[1-9][0-9]{0,15})$' OR (t.progress->>'total') !~ '^(0|[1-9][0-9]{0,15})$' THEN RETURN;END IF;
 d:=(t.progress->>'done')::bigint;n:=(t.progress->>'total')::bigint;
 IF d>n OR n>9007199254740991 THEN RETURN;END IF;
 SELECT started_at INTO started FROM photo.task_attempts WHERE tenant_id=t.tenant_id AND task_id=t.id
 AND attempt=p_attempt AND fencing_token=p_fence AND finished_at IS NULL;
 IF NOT FOUND OR started IS NULL THEN RETURN;END IF;
 SELECT * INTO prev FROM photo.outbox WHERE tenant_id=t.tenant_id AND destination='photo.events.v1'
 AND routing_key='task.progress' AND payload->>'task_id'=t.id::text ORDER BY created_at DESC,id DESC LIMIT 1;
 -- Task lock serializes emission; clock_timestamp avoids transaction-start timestamp rate bugs.
 IF FOUND THEN
  IF prev.created_at>stamp-interval '1 second' THEN RETURN;END IF;
  IF prev.payload->>'attempt'=p_attempt::text AND prev.payload->>'fencing_token'=p_fence::text THEN
   IF (prev.payload->'metrics'->>'total')::bigint<>n OR (prev.payload->'metrics'->>'done')::bigint>=d THEN RETURN;END IF;
  END IF;
 END IF;
 ev:=gen_random_uuid();
 INSERT INTO photo.outbox(created_at,tenant_id,event_id,destination,routing_key,payload)
 VALUES(stamp,t.tenant_id,ev,'photo.events.v1','task.progress',jsonb_build_object('schema_version',1,'event_id',ev,
 'type','task.progress','tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,'attempt',p_attempt,'fencing_token',p_fence,
 'outputs','[]'::jsonb,'error',NULL,'metrics',jsonb_build_object('duration_ms',greatest(0,extract(epoch FROM(stamp-started))*1000)::bigint,
 'peak_memory_bytes',0,'done',d,'total',n)));
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.emit_task_progress(uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.emit_task_progress(uuid,integer,bigint,uuid) FROM PUBLIC;
-- Promotion MUST strip default ACL grants and assert ONLY owner EXECUTE; no table grants.
-- +goose StatementBegin
DO $hook$ DECLARE old_meta jsonb;new_meta jsonb;BEGIN
 SELECT to_jsonb(p) INTO old_meta FROM pg_proc p WHERE oid='photo.heartbeat_task_pre_v23_series_device(uuid,integer,bigint,uuid,jsonb)'::regprocedure;
 IF old_meta->>'prosrc' IS DISTINCT FROM $old$
DECLARE t photo.tasks%ROWTYPE; until_time timestamptz; cancelled boolean; slot photo.resource_slots%ROWTYPE;
BEGIN
 IF p_progress IS NULL OR jsonb_typeof(p_progress)<>'object' OR octet_length(p_progress::text)>4096 THEN
 RAISE EXCEPTION 'INVALID_PROGRESS' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND tenant_id=t.tenant_id
 AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=p_task AND tenant_id=t.tenant_id;
 IF t.id IS NULL OR t.state<>'running' OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.lease_until<=clock_timestamp() OR (t.deadline IS NULL OR t.deadline<=clock_timestamp())
 OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.id IS NULL
 OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false; RETURN; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;

 -- 02537: live source after canonical locks, immediately before lease write.
 IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;END IF;
 -- A source predicate may itself wait on a registration row. Never revive an expired lease.
 IF t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp() THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false;RETURN;END IF;
 until_time:=least(clock_timestamp()+make_interval(secs=>photo.capacity_policy_task_lease_seconds(t.tenant_id,p_worker,t.timeout_seconds)),t.deadline);
 UPDATE photo.tasks SET lease_until=until_time,progress=p_progress WHERE id=t.id;
 UPDATE photo.resource_slots SET lease_until=until_time WHERE id=slot.id AND fencing_token=p_fence;
 UPDATE photo.resource_reservations SET expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id AND fencing_token=p_fence;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 RETURN QUERY SELECT true,until_time,cancelled;
END $old$ THEN RAISE EXCEPTION 'PROGRESS_HEARTBEAT_LEAF_PREIMAGE';END IF;
 EXECUTE $ddl$CREATE OR REPLACE FUNCTION photo.heartbeat_task_pre_v23_series_device(p_task uuid, p_attempt integer, p_fence bigint, p_worker uuid, p_progress jsonb)
 RETURNS TABLE(accepted boolean, lease_until timestamp with time zone, cancel_requested boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE t photo.tasks%ROWTYPE; until_time timestamptz; cancelled boolean; slot photo.resource_slots%ROWTYPE;
BEGIN
 IF p_progress IS NULL OR jsonb_typeof(p_progress)<>'object' OR octet_length(p_progress::text)>4096 THEN
 RAISE EXCEPTION 'INVALID_PROGRESS' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND tenant_id=t.tenant_id
 AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=p_task AND tenant_id=t.tenant_id;
 IF t.id IS NULL OR t.state<>'running' OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.lease_until<=clock_timestamp() OR (t.deadline IS NULL OR t.deadline<=clock_timestamp())
 OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.id IS NULL
 OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false; RETURN; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;

 -- 02537: live source after canonical locks, immediately before lease write.
 IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;END IF;
 -- A source predicate may itself wait on a registration row. Never revive an expired lease.
 IF t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp() THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false;RETURN;END IF;
 until_time:=least(clock_timestamp()+make_interval(secs=>photo.capacity_policy_task_lease_seconds(t.tenant_id,p_worker,t.timeout_seconds)),t.deadline);
 UPDATE photo.tasks SET lease_until=until_time,progress=p_progress WHERE id=t.id;
 UPDATE photo.resource_slots SET lease_until=until_time WHERE id=slot.id AND fencing_token=p_fence;
 UPDATE photo.resource_reservations SET expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id AND fencing_token=p_fence;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 PERFORM photo.emit_task_progress(p_task,p_attempt,p_fence,p_worker);
 RETURN QUERY SELECT true,until_time,cancelled;
END $function$
$ddl$;
 SELECT to_jsonb(p) INTO new_meta FROM pg_proc p WHERE oid='photo.heartbeat_task_pre_v23_series_device(uuid,integer,bigint,uuid,jsonb)'::regprocedure;
 IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' OR new_meta->>'prosrc' IS DISTINCT FROM $new$
DECLARE t photo.tasks%ROWTYPE; until_time timestamptz; cancelled boolean; slot photo.resource_slots%ROWTYPE;
BEGIN
 IF p_progress IS NULL OR jsonb_typeof(p_progress)<>'object' OR octet_length(p_progress::text)>4096 THEN
 RAISE EXCEPTION 'INVALID_PROGRESS' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND tenant_id=t.tenant_id
 AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=p_task AND tenant_id=t.tenant_id;
 IF t.id IS NULL OR t.state<>'running' OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.worker_id IS DISTINCT FROM p_worker OR t.lease_until<=clock_timestamp() OR (t.deadline IS NULL OR t.deadline<=clock_timestamp())
 OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.id IS NULL
 OR NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false; RETURN; END IF;
 SELECT t.cancel_requested OR r.cancel_requested INTO cancelled FROM photo.runs r WHERE r.id=t.run_id AND r.tenant_id=t.tenant_id;

 -- 02537: live source after canonical locks, immediately before lease write.
 IF photo.execution_source_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;END IF;
 -- A source predicate may itself wait on a registration row. Never revive an expired lease.
 IF t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
 OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
 OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp() THEN
 RETURN QUERY SELECT false,NULL::timestamptz,false;RETURN;END IF;
 until_time:=least(clock_timestamp()+make_interval(secs=>photo.capacity_policy_task_lease_seconds(t.tenant_id,p_worker,t.timeout_seconds)),t.deadline);
 UPDATE photo.tasks SET lease_until=until_time,progress=p_progress WHERE id=t.id;
 UPDATE photo.resource_slots SET lease_until=until_time WHERE id=slot.id AND fencing_token=p_fence;
 UPDATE photo.resource_reservations SET expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id AND fencing_token=p_fence;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 PERFORM photo.emit_task_progress(p_task,p_attempt,p_fence,p_worker);
 RETURN QUERY SELECT true,until_time,cancelled;
END $new$ THEN RAISE EXCEPTION 'PROGRESS_HEARTBEAT_METADATA_DRIFT';END IF;
END $hook$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;a record;allowed oid;BEGIN
 FOREACH f IN ARRAY ARRAY['photo.accept_orchestrator_event(uuid,uuid,text,jsonb)'::regprocedure,'photo.record_event_dead_letter(text,text,jsonb)'::regprocedure,'photo.emit_task_progress(uuid,integer,bigint,uuid)'::regprocedure] LOOP
  EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC',f);
  FOR a IN SELECT DISTINCT x.grantee FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))x WHERE p.oid=f AND x.grantee<>p.proowner AND x.grantee<>0 LOOP
   EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I',f,pg_get_userbyid(a.grantee));
  END LOOP;
 END LOOP;
 GRANT EXECUTE ON FUNCTION photo.accept_orchestrator_event(uuid,uuid,text,jsonb) TO photo_event_consumer;
 GRANT EXECUTE ON FUNCTION photo.record_event_dead_letter(text,text,jsonb) TO photo_publisher;
 -- emit_task_progress remains owner-only. No worker/API/scheduler table or function grants.
 -- Assert no accidental default grants or broad role/table authority survived.
 FOREACH f IN ARRAY ARRAY['photo.accept_orchestrator_event(uuid,uuid,text,jsonb)'::regprocedure,'photo.record_event_dead_letter(text,text,jsonb)'::regprocedure,'photo.emit_task_progress(uuid,integer,bigint,uuid)'::regprocedure] LOOP
  allowed:=CASE WHEN f='photo.accept_orchestrator_event(uuid,uuid,text,jsonb)'::regprocedure THEN 'photo_event_consumer'::regrole::oid
   WHEN f='photo.record_event_dead_letter(text,text,jsonb)'::regprocedure THEN 'photo_publisher'::regrole::oid ELSE NULL END;
  IF EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner)))x
   WHERE p.oid=f AND (x.privilege_type<>'EXECUTE' OR x.is_grantable OR (x.grantee<>p.proowner AND x.grantee IS DISTINCT FROM allowed))) THEN
   RAISE EXCEPTION 'EVENT_FUNCTION_ACL_DRIFT';END IF;
 END LOOP;
 IF EXISTS(SELECT FROM pg_roles WHERE rolname='photo_event_consumer'AND(rolsuper OR rolcanlogin OR rolinherit OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls))
 OR EXISTS(SELECT FROM pg_auth_members WHERE roleid='photo_event_consumer'::regrole OR member='photo_event_consumer'::regrole)
 OR EXISTS(SELECT FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo'AND c.relkind IN('r','p','v','m','f')AND has_table_privilege('photo_event_consumer',c.oid,'SELECT,INSERT,UPDATE,DELETE')) THEN RAISE EXCEPTION 'EVENT_ROLE_AUTHORITY_DRIFT';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'FORWARD_ONLY';END $$;
-- +goose StatementEnd
