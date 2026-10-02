-- An unsealed draft cannot have been dispatched. Once its run reservation
-- expires, cancel its blocked tasks and release that reservation atomically.
-- Pending transport uploads retain their own reservations until the existing
-- fenced backend-document cleanup confirms object deletion.
-- +goose StatementBegin
CREATE FUNCTION photo.expire_v2_draft(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
 next_cursor bigint; event_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed THEN RETURN false; END IF;
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved' FOR UPDATE;
 IF q.id IS NULL OR q.expires_at>clock_timestamp() OR q.bytes<=0
  OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
   AND (t.state<>'blocked' OR t.attempt<>0 OR t.fencing_token<>0))
  OR r.revision>=9007199254740991 THEN RETURN false; END IF;
 UPDATE photo.tasks SET state='cancelled',cancel_requested=true,
  terminal_reason='v2_draft_expired',fencing_token=fencing_token+1
 WHERE tenant_id=p_tenant AND run_id=p_run AND state='blocked';
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'V2_DRAFT_QUOTA_INCONSISTENT' USING ERRCODE='23514'; END IF;
 UPDATE photo.quota_reservations SET bytes=0,state='released',expires_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=q.id;
 UPDATE photo.runs SET status='cancelled',cancel_requested=true,revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514'; END IF;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','v2.run.cancelled',
  jsonb_build_object('run_id',p_run,'status','cancelled','reason','draft_expired'));
 event_id:=gen_random_uuid();
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
 VALUES(p_tenant,event_id,'photo.events.v1','v2.run.cancelled',
  jsonb_build_object('schema_version',1,'event_id',event_id,'type','v2.run.cancelled',
   'tenant_id',p_tenant,'run_id',p_run,'status','cancelled','reason','draft_expired'));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.expire_v2_draft(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_v2_draft(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expire_v2_draft(uuid,uuid) TO photo_maintenance;

-- Claim one expired draft at a time; the fenced function rechecks everything.
-- +goose StatementBegin
CREATE FUNCTION photo.expire_one_v2_draft() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 SELECT r.tenant_id,r.id INTO candidate FROM photo.runs r
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id
  AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved'
 WHERE r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed
  AND q.expires_at<=clock_timestamp()
 ORDER BY q.expires_at,r.id LIMIT 1;
 IF candidate.id IS NULL THEN RETURN false; END IF;
 RETURN photo.expire_v2_draft(candidate.tenant_id,candidate.id);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.expire_one_v2_draft() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_one_v2_draft() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expire_one_v2_draft() TO photo_maintenance;
