-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v24_capture_adoption_params(p_tenant uuid,p_task uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256 FROM photo.tasks t JOIN photo.artifacts a
 ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='capture.assess'
 AND t.state='succeeded' AND a.state='ready' AND EXISTS(
  SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id
   AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v24_capture_adoption_params(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v24_capture_adoption_params(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v24_capture_adoption_params(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.adopt_capture_report(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text,p_params_jcs text,p_context_jcs text,p_report_jcs text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; w photo.v2_workflows%ROWTYPE; r photo.runs%ROWTYPE;
 params jsonb; context jsonb; report jsonb; root uuid; root_sha text;
 session_id uuid; version_id uuid; severity text; alert_id uuid; event_id uuid; cursor_value bigint;
BEGIN
 IF p_params_jcs IS NULL OR p_context_jcs IS NULL OR p_report_jcs IS NULL
  OR octet_length(p_params_jcs)>8388608 OR octet_length(p_context_jcs)>8388608
  OR octet_length(p_report_jcs)>8388608 THEN
  RAISE EXCEPTION 'CAPTURE_ADOPTION_INVALID' USING ERRCODE='22023'; END IF;
 -- Generic adoption acquires lifecycle/run/task locks and verifies the entire
 -- output inventory. Any subsequent failure rolls back its marker and links.
 IF NOT photo.adopt_v2_plan(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest) THEN RETURN false; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.operation<>'capture.assess' THEN RAISE EXCEPTION 'CAPTURE_OPERATION_INVALID' USING ERRCODE='23514'; END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=t.run_id;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id;
 IF w.workflow_type<>'capture' THEN RAISE EXCEPTION 'CAPTURE_WORKFLOW_INVALID' USING ERRCODE='23514'; END IF;
 SELECT (z.outputs->0->>'artifact_id')::uuid,z.outputs->0->>'sha256' INTO root,root_sha
  FROM photo.task_results z WHERE tenant_id=p_tenant AND id=p_result;
 params:=p_params_jcs::jsonb;context:=p_context_jcs::jsonb;report:=p_report_jcs::jsonb;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
   AND a.sha256=encode(sha256(convert_to(p_params_jcs,'UTF8')),'hex'))
  OR root_sha<>encode(sha256(convert_to(p_report_jcs,'UTF8')),'hex')
  OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
   AND i.artifact_id=(params->'context'->>'artifact_id')::uuid
   AND i.sha256=params->'context'->>'sha256'
   AND i.sha256=encode(sha256(convert_to(p_context_jcs,'UTF8')),'hex'))
  OR report->'binding' IS DISTINCT FROM params->'binding'
  OR report->>'schema_version'<>'1' OR context->>'schema_version'<>'1'
  OR jsonb_typeof(report->'checks')<>'array' OR jsonb_array_length(report->'checks') NOT BETWEEN 1 AND 600
 THEN RAISE EXCEPTION 'CAPTURE_ADOPTION_BYTES_INVALID' USING ERRCODE='23514'; END IF;
 session_id:=(context->>'session_id')::uuid;version_id:=(report->'binding'->>'asset_version_id')::uuid;
 IF NOT EXISTS(SELECT 1 FROM photo.shoot_sessions s WHERE s.tenant_id=p_tenant AND s.project_id=w.project_id AND s.id=session_id)
  OR NOT EXISTS(SELECT 1 FROM photo.capture_receipts c JOIN photo.capture_receipt_assets a
   ON a.tenant_id=c.tenant_id AND a.receipt_id=c.id WHERE c.tenant_id=p_tenant
   AND c.project_id=w.project_id AND c.session_id=session_id AND a.asset_version_id=version_id)
 THEN RAISE EXCEPTION 'CAPTURE_RECEIPT_BINDING_INVALID' USING ERRCODE='23514'; END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(report->'checks') c
  WHERE coalesce(c->>'status','') NOT IN('pass','warning','critical','unavailable')) THEN
  RAISE EXCEPTION 'CAPTURE_STATUS_INVALID' USING ERRCODE='23514'; END IF;
 SELECT CASE WHEN bool_or(c->>'status'='critical') THEN 'critical'
  WHEN bool_or(c->>'status' IN('warning','unavailable')) THEN 'warning' ELSE 'info' END
  INTO severity FROM jsonb_array_elements(report->'checks') c;
 INSERT INTO photo.capture_alerts(tenant_id,project_id,session_id,asset_version_id,report_artifact_id,severity)
 VALUES(p_tenant,w.project_id,session_id,version_id,root,severity) RETURNING id INTO alert_id;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant
  AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF cursor_value IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514'; END IF;
 event_id:=gen_random_uuid();
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,cursor_value,r.id,r.created_by,'principal','capture.alert',
  jsonb_build_object('alert_id',alert_id,'session_id',session_id,'asset_version_id',version_id,'severity',severity));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','capture.alert',jsonb_build_object('schema_version',1,
  'event_id',event_id,'type','capture.alert','tenant_id',p_tenant,'run_id',r.id,'alert_id',alert_id));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_capture_report(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_capture_report(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_capture_report(uuid,uuid,integer,bigint,uuid,jsonb,text,text,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
