-- +goose Up
ALTER TABLE photo.capture_alerts ADD COLUMN technical_quality double precision
 CHECK(technical_quality IS NULL OR (technical_quality BETWEEN 0 AND 1 AND technical_quality<>'NaN'::double precision));
ALTER TABLE photo.capture_alerts ADD COLUMN quality_metric text CHECK(quality_metric IS NULL OR quality_metric='capture.technical_quality/1');
ALTER TABLE photo.capture_alerts ADD COLUMN quality_report_sha text CHECK(quality_report_sha IS NULL OR quality_report_sha~'^[0-9a-f]{64}$');
ALTER TABLE photo.capture_alerts ADD CONSTRAINT capture_quality_binding CHECK(
 (quality_metric IS NULL)=(quality_report_sha IS NULL) AND (technical_quality IS NULL OR quality_metric IS NOT NULL));
-- +goose StatementBegin
CREATE FUNCTION photo.guard_capture_quality() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF (NEW.technical_quality,NEW.quality_metric,NEW.quality_report_sha,NEW.report_artifact_id,NEW.asset_version_id)
 IS DISTINCT FROM (OLD.technical_quality,OLD.quality_metric,OLD.quality_report_sha,OLD.report_artifact_id,OLD.asset_version_id) THEN
  RAISE EXCEPTION 'immutable capture quality' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_capture_quality() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_capture_quality() FROM PUBLIC;
CREATE TRIGGER capture_quality_immutable BEFORE UPDATE ON photo.capture_alerts FOR EACH ROW EXECUTE FUNCTION photo.guard_capture_quality();

-- +goose StatementBegin
CREATE FUNCTION photo.capture_technical_quality(p_report jsonb,p_policy jsonb) RETURNS double precision
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE requested text[]; actual text[]; check_row jsonb; keys text[]:='{}'; key text;
 focus_count integer:=0; clipping_count integer:=0; incomplete boolean:=false;
 score double precision; quality double precision:=1;
BEGIN
 SELECT array_agg(DISTINCT value ORDER BY value) INTO requested FROM jsonb_array_elements_text(p_policy->'checks');
 SELECT array_agg(DISTINCT value->>'code' ORDER BY value->>'code') INTO actual FROM jsonb_array_elements(p_report->'checks');
 IF actual IS DISTINCT FROM requested THEN RAISE EXCEPTION 'capture quality check coverage' USING ERRCODE='23514'; END IF;
 FOR check_row IN SELECT value FROM jsonb_array_elements(p_report->'checks') LOOP
  IF check_row->>'code' NOT IN('focus','clipping','eyes','glare') THEN CONTINUE; END IF;
  key:=(check_row->>'code')||'/'||coalesce(check_row->>'face_id','frame');
  IF key=ANY(keys) THEN RAISE EXCEPTION 'duplicate capture quality check' USING ERRCODE='23514'; END IF;
  keys:=array_append(keys,key);
  IF check_row->>'face_id' IS NULL THEN
   IF check_row->>'code'='focus' THEN focus_count:=focus_count+1; END IF;
   IF check_row->>'code'='clipping' THEN clipping_count:=clipping_count+1; END IF;
  END IF;
  IF check_row->>'status'='unavailable' OR check_row->>'score' IS NULL THEN incomplete:=true; CONTINUE; END IF;
  score:=(check_row->>'score')::double precision;
  IF NOT score BETWEEN 0 AND 1 OR score='NaN'::double precision THEN
   RAISE EXCEPTION 'invalid capture quality score' USING ERRCODE='23514';
  END IF;
  IF check_row->>'code' IN('clipping','glare') THEN score:=1-score; END IF;
  quality:=least(quality,score);
 END LOOP;
 IF NOT ARRAY['focus','clipping']::text[]<@requested OR incomplete OR focus_count<>1 OR clipping_count<>1 THEN RETURN NULL; END IF;
 RETURN quality;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_technical_quality(jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_technical_quality(jsonb,jsonb) FROM PUBLIC;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_capture_report(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
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
 INSERT INTO photo.capture_alerts(tenant_id,project_id,session_id,asset_version_id,report_artifact_id,severity,technical_quality,quality_metric,quality_report_sha)
 VALUES(p_tenant,w.project_id,session_id,version_id,root,severity,photo.capture_technical_quality(report,params->'policy'),'capture.technical_quality/1',root_sha) RETURNING id INTO alert_id;
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.acknowledge_capture_alert(p_tenant uuid,p_id uuid,p_expected bigint,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE alert photo.capture_alerts%ROWTYPE;
BEGIN
 IF p_expected IS NULL OR p_expected<1 THEN
  RAISE EXCEPTION 'CAPTURE_ALERT_INVALID' USING ERRCODE='22023'; END IF;
 SELECT * INTO alert FROM photo.capture_alerts WHERE tenant_id=p_tenant AND id=p_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'CAPTURE_ALERT_MISSING' USING ERRCODE='P0002'; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,alert.project_id,p_actor,p_session) THEN
  RAISE EXCEPTION 'CAPTURE_ALERT_FORBIDDEN' USING ERRCODE='42501'; END IF;
 SELECT * INTO alert FROM photo.capture_alerts WHERE tenant_id=p_tenant AND id=p_id FOR UPDATE;
 IF alert.revision<>p_expected OR alert.acknowledged_at IS NOT NULL THEN
  RAISE EXCEPTION 'CAPTURE_ALERT_REVISION_CONFLICT' USING ERRCODE='40001'; END IF;
 UPDATE photo.capture_alerts SET acknowledged_by=p_actor,acknowledged_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=p_id RETURNING * INTO alert;
 RETURN (to_jsonb(alert)-ARRAY['technical_quality','quality_metric','quality_report_sha'])||jsonb_build_object('resource_type','capture_alerts','revision',alert.revision::text);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.acknowledge_capture_alert(uuid,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.acknowledge_capture_alert(uuid,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.acknowledge_capture_alert(uuid,uuid,bigint,uuid,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
