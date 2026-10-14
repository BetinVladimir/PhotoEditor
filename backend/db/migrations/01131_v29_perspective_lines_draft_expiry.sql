-- +goose Up
-- Expiry cancels publication authority but retains run bytes until existing
-- document/output cleanup and the dedicated terminal quota pass have settled.
-- +goose StatementBegin
ALTER FUNCTION photo.expire_v2_draft(uuid,uuid) RENAME TO expire_v2_draft_pre_v29_lines;
REVOKE ALL ON FUNCTION photo.expire_v2_draft_pre_v29_lines(uuid,uuid) FROM PUBLIC,photo_maintenance;
CREATE FUNCTION photo.expire_v2_draft(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;next_cursor bigint;event_id uuid;
BEGIN
 -- Flag is immutable; all actual decisions are repeated after scoped locks.
 IF NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND v29_perspective_line_detection) THEN
  RETURN photo.expire_v2_draft_pre_v29_lines(p_tenant,p_run);END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR NOT r.v29_perspective_line_detection OR r.kind<>'v2' OR r.status<>'draft'
 OR r.graph_sealed OR r.cancel_requested OR r.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF FOUND THEN
  IF q.state<>'reserved' OR q.expires_at>clock_timestamp() OR q.bytes<>4194304 THEN RETURN false;END IF;
 ELSE
  IF r.created_at>clock_timestamp()-interval '1 hour' THEN RETURN false;END IF;
 END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
 AND (state<>'blocked' OR attempt<>0 OR fencing_token<>0 OR canonical_result_id IS NOT NULL)) THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='cancelled',cancel_requested=true,terminal_reason='v29_lines_draft_expired',fencing_token=fencing_token+1
 WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET status='cancelled',cancel_requested=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=p_tenant
 AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','v2.run.cancelled',
 jsonb_build_object('run_id',p_run,'status','cancelled','reason','draft_expired'));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.run.cancelled',jsonb_build_object('schema_version',1,
 'event_id',event_id,'type','v2.run.cancelled','tenant_id',p_tenant,'run_id',p_run,'status','cancelled','reason','draft_expired'));
 RETURN true;
END $$;
ALTER FUNCTION photo.expire_v2_draft(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_v2_draft(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expire_v2_draft(uuid,uuid) TO photo_maintenance;

CREATE FUNCTION photo.expire_next_v29_perspective_lines_draft() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 LEFT JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 WHERE r.v29_perspective_line_detection AND r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
 AND ((q.state='reserved' AND q.bytes=4194304 AND q.expires_at<=clock_timestamp())
 OR (q.id IS NULL AND r.created_at<=clock_timestamp()-interval '1 hour'))
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
 AND (t.state<>'blocked' OR t.attempt<>0 OR t.fencing_token<>0 OR t.canonical_result_id IS NOT NULL))
 ORDER BY r.created_at,r.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.expire_v2_draft(candidate.tenant_id,candidate.id);
END $$;
ALTER FUNCTION photo.expire_next_v29_perspective_lines_draft() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_next_v29_perspective_lines_draft() FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.expire_next_v29_perspective_lines_draft() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
