-- +goose Up
-- A review can become terminal without a user decision when its owning run is
-- cancelled. Decision fields remain NULL so cancellation cannot be mistaken
-- for an editorial accept/reject/skip decision.
ALTER TABLE photo.reviews DROP CONSTRAINT reviews_domain_0;
ALTER TABLE photo.reviews DROP CONSTRAINT reviews_domain_2;
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_domain_0
 CHECK(state IN ('pending','decided','cancelled'));
ALTER TABLE photo.reviews ADD CONSTRAINT reviews_domain_2 CHECK(
 (state='pending' AND decision IS NULL AND decided_by IS NULL AND decided_at IS NULL) OR
 (state='decided' AND decision IS NOT NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL) OR
 (state='cancelled' AND decision IS NULL AND decided_by IS NULL AND decided_at IS NULL));

-- Finalize one run per transaction. Idle tasks are drained by SQL73 and live
-- attempts by worker heartbeat/reaper before this function can succeed.
-- Ingest is deliberately held until every upload has a settled catalog or
-- rejection receipt: cancellation may not silently release or lose its quota.
-- +goose StatementBegin
CREATE FUNCTION photo.finalize_run_cancellation() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate uuid; r photo.runs%ROWTYPE; next_cursor bigint;
BEGIN
 SELECT rr.id INTO candidate FROM photo.runs rr
 WHERE rr.cancel_requested AND rr.status='cancelling'
 AND NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=rr.tenant_id AND t.run_id=rr.id
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped'))
 ORDER BY rr.created_at,rr.id LIMIT 1;
 IF candidate IS NULL THEN RETURN 0;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=candidate FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR NOT r.cancel_requested OR r.status<>'cancelling' THEN RETURN 0;END IF;
 IF EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN 0;END IF;
 IF r.kind='ingest' AND EXISTS(
  SELECT FROM photo.ingest_upload_bindings b
  LEFT JOIN photo.ingest_catalog_items c ON c.tenant_id=b.tenant_id AND c.run_id=b.run_id AND c.ordinal=b.ordinal AND c.settled_at IS NOT NULL
  LEFT JOIN photo.ingest_rejections f ON f.tenant_id=b.tenant_id AND f.run_id=b.run_id AND f.ordinal=b.ordinal
  WHERE b.tenant_id=r.tenant_id AND b.run_id=r.id AND c.ordinal IS NULL AND f.ordinal IS NULL
 ) THEN RETURN 0;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;

 UPDATE photo.reviews SET state='cancelled',revision=revision+1
 WHERE tenant_id=r.tenant_id AND run_id=r.id AND state='pending' AND revision<9007199254740991;
 IF EXISTS(SELECT FROM photo.reviews WHERE tenant_id=r.tenant_id AND run_id=r.id AND state='pending') THEN
  RAISE EXCEPTION 'REVIEW_REVISION_EXHAUSTED' USING ERRCODE='23514';
 END IF;
 UPDATE photo.graph_expansions SET state='closed',expansion_token=NULL,expansion_lease_until=NULL
 WHERE tenant_id=r.tenant_id AND run_id=r.id AND state<>'closed';

 IF r.kind='ingest' THEN
  INSERT INTO photo.ingest_progress(tenant_id,run_id,phase,item_count)
  SELECT r.tenant_id,r.id,'complete',s.item_count FROM photo.snapshots s
  WHERE s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
  ON CONFLICT(run_id) DO UPDATE SET phase='complete';
  IF NOT FOUND THEN RAISE EXCEPTION 'INGEST_SNAPSHOT_MISSING' USING ERRCODE='23514';END IF;
 END IF;

 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(r.tenant_id,0)
 ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1
 WHERE tenant_id=r.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO next_cursor;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514';END IF;
 UPDATE photo.runs SET status='cancelled',graph_sealed=true,revision=revision+1
 WHERE tenant_id=r.tenant_id AND id=r.id;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(r.tenant_id,next_cursor,r.id,r.created_by,'principal','run.cancelled',
  jsonb_build_object('run_id',r.id,'status','cancelled','revision',r.revision+1));
 RETURN 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finalize_run_cancellation() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_run_cancellation() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_run_cancellation() TO photo_scheduler,photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
