-- The service validates typed JSON closure bytes before this transaction.
-- This boundary rechecks every relational fact and publishes no partial result.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_adoption_event_id(p_task uuid,p_result uuid) RETURNS uuid
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE h bytea;
BEGIN
 h:=public.digest(uuid_send(p_task)||convert_to('adopt:'||p_result::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 RETURN encode(substring(h FROM 1 FOR 16),'hex')::uuid;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_adoption_event_id(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_adoption_event_id(uuid,uuid) FROM PUBLIC;

-- Inventory rows are {artifact_id,sha256,purpose,ordinal}; ordinal 0 is the
-- root. All newly produced members must have the same exact upload fence.
-- A service call uses the digest of its validated canonical inventory.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant_row photo.tenants%ROWTYPE;run_row photo.runs%ROWTYPE;task_row photo.tasks%ROWTYPE;
 result_row photo.task_results%ROWTYPE;workflow_row photo.v2_workflows%ROWTYPE;
 artifact_row photo.artifacts%ROWTYPE;upload_row photo.output_uploads%ROWTYPE;
 ref record; root jsonb; marker uuid; prior jsonb; sql_digest text; next_cursor bigint;
BEGIN
 IF p_digest IS NULL OR p_digest !~ '^[0-9a-f]{64}$' OR p_inventory IS NULL
  OR jsonb_typeof(p_inventory)<>'array' OR jsonb_array_length(p_inventory) NOT BETWEEN 1 AND 30001
  OR pg_column_size(p_inventory)>16777216 THEN
  RAISE EXCEPTION 'V2_ADOPTION_INVENTORY_INVALID' USING ERRCODE='22023';
 END IF;
 sql_digest:=encode(sha256(convert_to(p_inventory::text,'UTF8')),'hex');
 SELECT * INTO tenant_row FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF tenant_row.id IS NULL OR tenant_row.status<>'active' THEN RETURN false;END IF;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF task_row.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO run_row FROM photo.runs WHERE tenant_id=p_tenant AND id=task_row.run_id FOR UPDATE;
 SELECT * INTO task_row FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF run_row.id IS NULL OR run_row.kind<>'v2' OR task_row.run_id<>run_row.id
  OR task_row.canonical_result_id IS DISTINCT FROM p_result OR task_row.attempt IS DISTINCT FROM p_attempt
  OR task_row.fencing_token IS DISTINCT FROM p_fence THEN RETURN false;END IF;
 marker:=photo.v2_adoption_event_id(p_task,p_result);
 SELECT e.payload INTO prior FROM photo.inbox i JOIN photo.run_events e
  ON e.tenant_id=i.tenant_id AND e.run_id=run_row.id AND e.type='v2.task.adopted'
  AND e.payload->>'task_id'=p_task::text AND e.payload->>'result_id'=p_result::text
  WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=marker;
 IF EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt' AND i.event_id=marker) THEN
  IF prior->>'inventory_digest'=p_digest AND prior->>'sql_digest'=sql_digest THEN RETURN false;END IF;
  RAISE EXCEPTION 'V2_ADOPTION_RESULT_CONFLICT' USING ERRCODE='23514';
 END IF;
 IF run_row.status<>'running' OR run_row.cancel_requested OR task_row.cancel_requested
  OR task_row.state<>'succeeded' OR task_row.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2' THEN
  RETURN false;
 END IF;
 SELECT * INTO workflow_row FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.run_id=run_row.id AND w.renderer_version='v2.2';
 IF workflow_row.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO result_row FROM photo.task_results z WHERE z.tenant_id=p_tenant
  AND z.id=p_result AND z.task_id=p_task AND z.attempt=p_attempt AND z.fencing_token=p_fence;
 IF result_row.id IS NULL OR jsonb_typeof(result_row.outputs)<>'array'
  OR jsonb_array_length(result_row.outputs)<>1 THEN RETURN false;END IF;
 root:=result_row.outputs->0;
 IF p_inventory->0->>'artifact_id' IS DISTINCT FROM root->>'artifact_id'
  OR p_inventory->0->>'sha256' IS DISTINCT FROM root->>'sha256' THEN
  RAISE EXCEPTION 'V2_ADOPTION_ROOT_MISMATCH' USING ERRCODE='23514';
 END IF;
 IF EXISTS(
  WITH entries AS MATERIALIZED (
   SELECT (value->>'artifact_id')::uuid AS artifact_id FROM jsonb_array_elements(p_inventory)
    WHERE coalesce(value->>'artifact_id','') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  )
  SELECT 1 FROM photo.output_uploads u LEFT JOIN entries e ON e.artifact_id=u.artifact_id
   WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
    AND (u.state<>'ready' OR e.artifact_id IS NULL)
 ) THEN RAISE EXCEPTION 'V2_ADOPTION_OUTPUT_INCOMPLETE' USING ERRCODE='23514';END IF;
 FOR ref IN SELECT value,ordinality-1 AS ordinal FROM jsonb_array_elements(p_inventory) WITH ORDINALITY LOOP
  IF jsonb_typeof(ref.value)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(ref.value))<>4
   OR NOT (ref.value ?& ARRAY['artifact_id','sha256','purpose','ordinal'])
   OR coalesce(ref.value->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(ref.value->>'sha256','') !~ '^[0-9a-f]{64}$'
   OR coalesce(ref.value->>'purpose','') !~ '^[0-9a-f]{64}$'
   OR ref.value->>'ordinal' IS DISTINCT FROM ref.ordinal::text THEN
   RAISE EXCEPTION 'V2_ADOPTION_INVENTORY_INVALID' USING ERRCODE='22023';
  END IF;
  SELECT * INTO artifact_row FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=(ref.value->>'artifact_id')::uuid FOR SHARE;
  IF artifact_row.id IS NULL OR artifact_row.sha256<>ref.value->>'sha256'
   OR artifact_row.state<>'ready' OR artifact_row.verified_at IS NULL
   OR artifact_row.object_version IS NULL OR artifact_row.object_version IN ('','null')
   OR (artifact_row.expires_at IS NOT NULL AND artifact_row.expires_at<=clock_timestamp()) THEN
   RAISE EXCEPTION 'V2_ADOPTION_ARTIFACT_UNVERIFIED' USING ERRCODE='23514';
  END IF;
  IF artifact_row.created_by_task_id=p_task AND artifact_row.produced_attempt=p_attempt
   AND artifact_row.produced_fence=p_fence THEN
   SELECT * INTO upload_row FROM photo.output_uploads u WHERE u.tenant_id=p_tenant
    AND u.artifact_id=artifact_row.id AND u.task_id=p_task AND u.attempt=p_attempt
    AND u.fence=p_fence AND u.state='ready' AND u.sha256=artifact_row.sha256
    AND u.role=artifact_row.output_role FOR SHARE;
   IF upload_row.id IS NULL THEN
    RAISE EXCEPTION 'V2_ADOPTION_UPLOAD_UNVERIFIED' USING ERRCODE='23514';
   END IF;
  ELSIF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.project_id=workflow_row.project_id AND i.task_id=p_task
   AND i.artifact_id=artifact_row.id AND i.sha256=artifact_row.sha256) THEN
   RAISE EXCEPTION 'V2_ADOPTION_UNDECLARED_INPUT' USING ERRCODE='23514';
  END IF;
  INSERT INTO photo.v2_artifact_links(tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
   VALUES(p_tenant,workflow_row.project_id,workflow_row.id,artifact_row.id,
    artifact_row.sha256,ref.value->>'purpose',ref.ordinal);
 END LOOP;
 INSERT INTO photo.inbox(tenant_id,consumer,event_id,processed_at)
  VALUES(p_tenant,'v2-adopt',marker,clock_timestamp());
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
  VALUES(p_tenant,next_cursor,run_row.id,run_row.created_by,'principal','v2.task.adopted',
   jsonb_build_object('task_id',p_task,'result_id',p_result,'attempt',p_attempt,'fence',p_fence,
    'inventory_digest',p_digest,'sql_digest',sql_digest,'inventory_count',jsonb_array_length(p_inventory)));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
  VALUES(p_tenant,marker,'photo.events.v1','v2.task.adopted',
   jsonb_build_object('schema_version',1,'event_id',marker,'type','v2.task.adopted',
    'tenant_id',p_tenant,'run_id',run_row.id,'task_id',p_task,'result_id',p_result));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;

-- A semantic validation failure stops new work but lets existing leases be
-- fenced by the normal cancellation path. The finalizer reports failed once
-- every task is terminal; this event distinguishes it from a user cancel.
-- +goose StatementBegin
CREATE FUNCTION photo.reject_v2_adoption(p_tenant uuid,p_task uuid,p_result uuid,p_code text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant_row photo.tenants%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;
 next_cursor bigint;ev uuid:=gen_random_uuid();
BEGIN
 IF p_code IS NULL OR p_code !~ '^[A-Z][A-Z0-9_]{0,79}$' THEN
  RAISE EXCEPTION 'V2_ADOPTION_FAILURE_CODE_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO tenant_row FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF tenant_row.id IS NULL OR tenant_row.status<>'active' THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.cancel_requested
  OR t.state<>'succeeded' OR t.canonical_result_id IS DISTINCT FROM p_result
  OR EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1
  WHERE tenant_id=p_tenant AND id=r.id;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
  VALUES(p_tenant,next_cursor,r.id,r.created_by,'principal','v2.adoption.failed',
   jsonb_build_object('task_id',p_task,'result_id',p_result,'code',p_code));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
  VALUES(p_tenant,ev,'photo.events.v1','v2.adoption.failed',
   jsonb_build_object('schema_version',1,'event_id',ev,'type','v2.adoption.failed',
    'tenant_id',p_tenant,'run_id',r.id,'task_id',p_task,'code',p_code));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reject_v2_adoption(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_v2_adoption(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reject_v2_adoption(uuid,uuid,uuid,text) TO photo_scheduler;
