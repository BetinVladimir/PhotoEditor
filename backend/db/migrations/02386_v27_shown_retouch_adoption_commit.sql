-- +goose Up
-- Retouch copy retains exact canonical bytes under lifecycle locks.
-- Dedicated Apply-child inventory retention under original semantic workflow.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_shown_retouch_inventory(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
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
 IF run_row.id IS NULL OR run_row.kind<>'apply' OR task_row.operation<>'semantic.adopt_shown_retouch_region' OR task_row.operation_version<>'1' OR task_row.run_id<>run_row.id
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
 SELECT w.* INTO workflow_row FROM photo.v2_workflows w
 JOIN photo.semantic_intents i ON i.tenant_id=w.tenant_id AND i.workflow_id=w.id
 JOIN photo.semantic_targets target ON target.tenant_id=i.tenant_id AND target.intent_id=i.id
 WHERE w.tenant_id=p_tenant AND target.child_run_id=run_row.id AND target.state='queued'
 AND target.id=(SELECT (n->>'target_id')::uuid FROM jsonb_array_elements(run_row.config_snapshot->'shown_semantic'->'nodes') n WHERE n->>'task_id'=p_task::text)
 AND w.project_id=(run_row.config_snapshot->>'project_id')::uuid AND w.renderer_version='v2.2';
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
ALTER FUNCTION photo.adopt_shown_retouch_inventory(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_shown_retouch_inventory(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
REVOKE ALL ON FUNCTION photo.adopt_shown_retouch_inventory(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM photo_scheduler;

-- +goose StatementEnd

-- Preserve all installed adoption gates; shown-copy requires a dedicated commit.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_before_shown_retouch;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_before_shown_retouch(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
 AND operation='semantic.adopt_shown_retouch_region') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_before_shown_retouch(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd

-- Private output proof. Caller supplies original version-pinned JSON bytes;
-- metadata SHA/size and canonical result membership are checked independently.
-- +goose StatementBegin
CREATE FUNCTION photo.shown_retouch_output_bound(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_params uuid,p_sha text,p_body text,p_root uuid,p_root_sha text,p_output_body text,p_shown_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e jsonb;shown jsonb;output jsonb;root_row photo.artifacts%ROWTYPE;
 shown_row photo.artifacts%ROWTYPE;original photo.artifacts%ROWTYPE;copied photo.artifacts%ROWTYPE;
BEGIN
 IF photo.v27_shown_retouch_adoption_ready(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body)
 IS DISTINCT FROM true THEN RETURN false;END IF;
 IF p_output_body IS NULL OR p_shown_body IS NULL OR octet_length(p_output_body) NOT BETWEEN 2 AND 1048576
 OR octet_length(p_shown_body) NOT BETWEEN 2 AND 1048576 OR p_root IS NULL OR p_root_sha IS NULL
 OR p_root_sha !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 e:=p_body::jsonb;shown:=p_shown_body::jsonb;output:=p_output_body::jsonb;
 IF jsonb_typeof(shown) IS DISTINCT FROM 'object' OR jsonb_typeof(output) IS DISTINCT FROM 'object'
 OR shown->'pixels' IS DISTINCT FROM e->'pixels'
 OR (shown-'pixels') IS DISTINCT FROM (output-'pixels')
 OR output->'pixels'->>'sha256' IS DISTINCT FROM shown->'pixels'->>'sha256'
 OR output->'pixels'->>'artifact_id' IS NULL OR jsonb_typeof(output->'pixels') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(output->'pixels'))<>2
 OR NOT(output->'pixels' ?& ARRAY['artifact_id','sha256'])
 OR p_root='00000000-0000-0000-0000-000000000000'::uuid
 OR (output->'pixels'->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR p_root=(output->'pixels'->>'artifact_id')::uuid
 OR p_root IN(p_params,(e->'working_image'->>'artifact_id')::uuid,(e->'pixels'->>'artifact_id')::uuid)
 OR (output->'pixels'->>'artifact_id')::uuid IN(p_params,(e->'working_image'->>'artifact_id')::uuid,(e->'pixels'->>'artifact_id')::uuid)
 OR p_root::text IS NOT DISTINCT FROM shown->'alpha'->>'artifact_id'
 OR output->'pixels'->>'artifact_id' IS NOT DISTINCT FROM shown->'alpha'->>'artifact_id'
 THEN RETURN false;END IF;
 SELECT * INTO root_row FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root FOR SHARE;
 SELECT * INTO shown_row FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(e->'working_image'->>'artifact_id')::uuid FOR SHARE;
 SELECT * INTO original FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(e->'pixels'->>'artifact_id')::uuid FOR SHARE;
 SELECT * INTO copied FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(output->'pixels'->>'artifact_id')::uuid FOR SHARE;
 IF root_row.id IS NULL OR shown_row.id IS NULL OR original.id IS NULL OR copied.id IS NULL
 OR root_row.sha256 IS DISTINCT FROM p_root_sha
 OR root_row.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_output_body,'UTF8'),'sha256'),'hex')
 OR shown_row.sha256 IS DISTINCT FROM e->'working_image'->>'sha256'
 OR shown_row.sha256 IS DISTINCT FROM encode(public.digest(convert_to(p_shown_body,'UTF8'),'sha256'),'hex')
 OR root_row.bytes IS DISTINCT FROM octet_length(p_output_body)::bigint
 OR shown_row.bytes IS DISTINCT FROM octet_length(p_shown_body)::bigint
 OR root_row.media_type IS DISTINCT FROM 'application/json' OR shown_row.media_type IS DISTINCT FROM 'application/json'
 OR copied.media_type IS DISTINCT FROM 'application/x-npy' OR original.media_type IS DISTINCT FROM 'application/x-npy'
 OR original.sha256 IS DISTINCT FROM e->'pixels'->>'sha256' OR copied.sha256 IS DISTINCT FROM original.sha256
 OR copied.bytes IS DISTINCT FROM original.bytes OR original.bytes NOT BETWEEN 1 AND 2400016384
 OR root_row.created_by_task_id IS DISTINCT FROM p_task OR copied.created_by_task_id IS DISTINCT FROM p_task
 OR root_row.produced_attempt IS DISTINCT FROM p_attempt OR copied.produced_attempt IS DISTINCT FROM p_attempt
 OR root_row.produced_fence IS DISTINCT FROM p_fence OR copied.produced_fence IS DISTINCT FROM p_fence
 OR root_row.output_role IS DISTINCT FROM 'working_image' OR copied.output_role IS DISTINCT FROM 'pixels'
 THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
 AND a.id IN(root_row.id,shown_row.id,original.id,copied.id)
 AND (a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results z WHERE z.tenant_id=p_tenant AND z.task_id=p_task
 AND z.id=p_result AND z.attempt=p_attempt AND z.fencing_token=p_fence
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',p_root,'sha256',p_root_sha))) THEN RETURN false;END IF;
 RETURN NOT EXISTS(SELECT 1 FROM (VALUES(root_row.id,root_row.sha256,'working_image'),
 (copied.id,copied.sha256,'pixels')) required(id,sha,role)
 WHERE NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.task_id=p_task
 AND u.attempt=p_attempt AND u.fence=p_fence AND u.artifact_id=required.id
 AND u.sha256=required.sha AND u.role=required.role AND u.state='ready'));
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.shown_retouch_output_bound(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.shown_retouch_output_bound(uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
-- +goose StatementEnd

-- Atomic inventory retention after repeated shown-copy proof under locks.
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_shown_retouch(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text,p_params uuid,p_sha text,p_body text,
 p_root uuid,p_root_sha text,p_output_body text,p_shown_body text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;project uuid;task_ids uuid[];e jsonb;accepted boolean;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576
 OR p_output_body IS NULL OR octet_length(p_output_body) NOT BETWEEN 2 AND 1048576
 OR p_shown_body IS NULL OR octet_length(p_shown_body) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 e:=p_body::jsonb;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL OR t.operation IS DISTINCT FROM 'semantic.adopt_shown_retouch_region'
 OR t.operation_version IS DISTINCT FROM '1' THEN RETURN false;END IF;
 SELECT (r.config_snapshot->>'project_id')::uuid INTO project FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=t.run_id;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=project AND archived_at IS NULL FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT array_agg(DISTINCT id ORDER BY id) INTO task_ids FROM (
 SELECT p_task AS id UNION ALL SELECT a.created_by_task_id FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND (a.id IN(p_root,(p_output_body::jsonb->'pixels'->>'artifact_id')::uuid)
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=a.id))
 UNION ALL SELECT (e->>'preview_task_id')::uuid) ids WHERE id IS NOT NULL;
 PERFORM r.id FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id IN(
 SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids)) ORDER BY r.id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=ANY(task_ids) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.semantic_targets WHERE tenant_id=p_tenant AND id=(e->>'target_id')::uuid FOR UPDATE;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (a.id IN(p_params,p_root,
 (p_output_body::jsonb->'pixels'->>'artifact_id')::uuid) OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=a.id)) ORDER BY a.id FOR SHARE;
 IF photo.shown_retouch_output_bound(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body,
 p_root,p_root_sha,p_output_body,p_shown_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 accepted:=photo.adopt_shown_retouch_inventory(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
 IF accepted AND photo.shown_retouch_output_bound(p_tenant,p_task,p_attempt,p_fence,p_result,p_params,p_sha,p_body,
 p_root,p_root_sha,p_output_body,p_shown_body) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'SEMANTIC_SHOWN_ADOPTION_STALE' USING ERRCODE='23514';END IF;
 RETURN accepted;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.adopt_shown_retouch(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_shown_retouch(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.adopt_shown_retouch(uuid,uuid,integer,bigint,uuid,jsonb,text,uuid,text,text,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
