-- +goose Up
-- Generic V2 finalization consumes/releases quota before output cleanup. Lines
-- must use their dedicated lifecycle and retain the reservation until settled.
-- +goose StatementBegin
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) RENAME TO finalize_v2_run_pre_v29_lines;
REVOKE ALL ON FUNCTION photo.finalize_v2_run_pre_v29_lines(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF r.v29_perspective_line_detection THEN
  IF r.status IN('completed','completed_with_errors','failed','cancelled') THEN RETURN r.status;END IF;
  RETURN 'pending';
 END IF;
 RETURN photo.finalize_v2_run_pre_v29_lines(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;

CREATE FUNCTION photo.finalize_failed_v29_perspective_lines()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;r photo.runs%ROWTYPE;next_cursor bigint;
BEGIN
 SELECT x.tenant_id,x.id INTO candidate FROM photo.runs x
 WHERE x.v29_perspective_line_detection AND x.kind='v2' AND x.graph_sealed
 AND NOT x.cancel_requested AND x.status IN('queued','running')
 AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=x.tenant_id AND t.run_id=x.id AND t.state IN('failed','cancelled','skipped'))
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=x.tenant_id AND t.run_id=x.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 ORDER BY x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.cancel_requested OR NOT r.graph_sealed
 OR r.status NOT IN('queued','running') THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id FOR UPDATE;
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id)<>1
 OR NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=r.tenant_id AND run_id=r.id
 AND operation='optical.perspective_lines' AND state IN('failed','cancelled','skipped')) THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=r.tenant_id AND id=r.id;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=r.tenant_id
 AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(r.tenant_id,next_cursor,r.id,r.created_by,'principal','v2.run.failed',jsonb_build_object('run_id',r.id,'status','failed'));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(r.tenant_id,r.id,'photo.events.v1','v2.run.failed',jsonb_build_object('schema_version',1,'event_id',r.id,
 'type','v2.run.failed','tenant_id',r.tenant_id,'run_id',r.id,'status','failed'));
 RETURN true;
END $$;
ALTER FUNCTION photo.finalize_failed_v29_perspective_lines() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_failed_v29_perspective_lines() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.finalize_failed_v29_perspective_lines() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
