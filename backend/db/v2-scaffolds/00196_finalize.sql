-- A canonical result is not publishable until its deterministic adoption
-- marker exists. One run is finalized per transaction after all tasks settle.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant_row photo.tenants%ROWTYPE;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 next_state text;next_cursor bigint;ev uuid;has_review boolean;
BEGIN
 SELECT * INTO tenant_row FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF tenant_row.id IS NULL OR tenant_row.status<>'active' THEN RETURN 'stale';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' THEN RETURN 'stale';END IF;
 IF r.status IN ('completed','failed','cancelled','waiting_review') THEN RETURN r.status;END IF;
 IF r.status NOT IN ('queued','running','cancelling') OR NOT r.graph_sealed THEN RETURN 'pending';END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN 'pending';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run) THEN RETURN 'pending';END IF;
 IF r.cancel_requested OR r.status='cancelling' THEN
  UPDATE photo.graph_expansions SET state='closed',expansion_token=NULL,expansion_lease_until=NULL
   WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'closed';
 ELSIF EXISTS(SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant
  AND e.run_id=p_run AND e.state<>'closed') THEN RETURN 'pending';END IF;
 IF r.cancel_requested OR r.status='cancelling' THEN
  next_state:=CASE WHEN EXISTS(SELECT 1 FROM photo.run_events e WHERE e.tenant_id=p_tenant
   AND e.run_id=p_run AND e.type='v2.adoption.failed') THEN 'failed' ELSE 'cancelled' END;
 ELSIF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.state IN ('failed','cancelled','skipped')) THEN
  next_state:='failed';
 ELSE
  IF r.status<>'running' OR EXISTS(SELECT 1 FROM photo.tasks t
   WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND (t.state<>'succeeded'
    OR t.canonical_result_id IS NULL
    OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
     AND i.consumer='v2-adopt'
     AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)))) THEN
   RETURN 'pending';
  END IF;
  SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z
   ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND z.review_required) INTO has_review;
  next_state:=CASE WHEN has_review THEN 'waiting_review' ELSE 'completed' END;
 END IF;
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved' FOR UPDATE;
 IF q.id IS NOT NULL THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes
   WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'V2_RESERVATION_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET bytes=0,
   state=CASE WHEN next_state IN ('failed','cancelled') THEN 'released' ELSE 'consumed' END
   WHERE tenant_id=p_tenant AND id=q.id;
 END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status=next_state,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(p_tenant,0)
  ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
  WHERE tenant_id=p_tenant AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF next_cursor IS NULL THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 ev:=p_run;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
  VALUES(p_tenant,next_cursor,p_run,r.created_by,'principal','v2.run.'||next_state,
   jsonb_build_object('run_id',p_run,'status',next_state));
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload)
  VALUES(p_tenant,ev,'photo.events.v1','v2.run.'||next_state,
   jsonb_build_object('schema_version',1,'event_id',ev,'type','v2.run.'||next_state,
    'tenant_id',p_tenant,'run_id',p_run,'status',next_state));
 RETURN next_state;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;
