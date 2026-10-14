-- +goose Up
-- Detection produces editable evidence, not an Apply proposal. A committed
-- available or unsupported result finishes its producer without changing images.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_next_v29_perspective_lines()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;prior text;next_cursor bigint;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 WHERE x.v29_perspective_line_detection AND x.kind='v2' AND x.graph_sealed
 AND NOT x.cancel_requested AND x.status IN('running','waiting_review')
 AND x.v29_perspective_lines_adoption_receipt IS NOT NULL
 AND (SELECT count(*) FROM photo.tasks task WHERE task.tenant_id=x.tenant_id AND task.run_id=x.id)=1
 AND EXISTS(SELECT 1 FROM photo.tasks task WHERE task.tenant_id=x.tenant_id AND task.run_id=x.id
 AND task.operation='optical.perspective_lines' AND task.state='succeeded' AND NOT task.cancel_requested)
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN('running','waiting_review')
 OR NOT r.v29_perspective_line_detection OR r.v29_perspective_lines_adoption_receipt IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id)<>1 THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id;
 IF t.state<>'succeeded' OR t.cancel_requested OR t.operation<>'optical.perspective_lines'
 OR t.id IS DISTINCT FROM (r.v29_perspective_lines_adoption_receipt->>'task_id')::uuid
 OR t.canonical_result_id IS DISTINCT FROM (r.v29_perspective_lines_adoption_receipt->>'result_id')::uuid THEN RETURN false;END IF;
 prior:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',r.tenant_id::text,true);
 IF photo.v29_perspective_lines_adoption_committed(r.tenant_id,t.id) IS DISTINCT FROM true THEN
  PERFORM set_config('app.tenant_id',coalesce(prior,''),true);RETURN false;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='completed',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
 WHERE tenant_id=r.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(r.tenant_id,next_cursor,r.id,r.created_by,'principal','v2.run.completed',jsonb_build_object('run_id',r.id,'status','completed'));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(r.tenant_id,r.id,'photo.events.v1','v2.run.completed',jsonb_build_object('schema_version',1,'event_id',r.id,
 'type','v2.run.completed','tenant_id',r.tenant_id,'run_id',r.id,'status','completed'));
 PERFORM set_config('app.tenant_id',coalesce(prior,''),true);
 RETURN true;
END $$;
ALTER FUNCTION photo.finalize_next_v29_perspective_lines() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_next_v29_perspective_lines() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.finalize_next_v29_perspective_lines() TO photo_scheduler;

CREATE FUNCTION photo.release_next_v29_perspective_lines_quota()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;prior text;released boolean;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved'
 WHERE r.v29_perspective_line_detection AND r.kind='v2' AND r.status IN('completed','completed_with_errors','failed','cancelled')
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND u.state NOT IN('ready','cleaned'))
 AND NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.state NOT IN('ready','cleaned'))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 prior:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',candidate.tenant_id::text,true);
 released:=photo.release_v29_perspective_line_detection_quota(candidate.tenant_id,candidate.id);
 PERFORM set_config('app.tenant_id',coalesce(prior,''),true);
 RETURN released;
END $$;
ALTER FUNCTION photo.release_next_v29_perspective_lines_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_v29_perspective_lines_quota() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.release_next_v29_perspective_lines_quota() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
