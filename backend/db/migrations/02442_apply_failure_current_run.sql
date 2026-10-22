-- +goose Up
-- Failure must re-read current run authority under the cancellation row lock.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.fail_apply_build(p_run uuid,p_token uuid,p_error text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;
BEGIN
 -- Match Apply adoption/claim: build, run, reservation, usage. Generic Cancel
 -- locks run only, never apply_builds; cancellation finalization does likewise.
 SELECT * INTO b FROM photo.apply_builds WHERE run_id=p_run FOR UPDATE;
 IF b.run_id IS NULL OR b.phase IN('complete','failed') OR b.lease_token IS DISTINCT FROM p_token
  OR b.lease_until IS NULL OR b.lease_until<clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=b.tenant_id AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'apply' OR r.cancel_requested OR r.status NOT IN('queued','running') THEN RETURN false;END IF;
 -- Waiting for a row lock must not extend an expired failure authority.
 IF b.lease_until<clock_timestamp() THEN RETURN false;END IF;
 IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=b.tenant_id
  AND holder_type='run' AND holder_id=p_run AND state='reserved' FOR UPDATE;
 IF FOUND THEN
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=b.tenant_id AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'RUN_RESERVATION_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET bytes=0,state='released' WHERE tenant_id=b.tenant_id AND id=q.id;
 END IF;
 UPDATE photo.apply_builds SET phase='failed',lease_token=NULL,lease_until=NULL,last_error=left(p_error,2000),updated_at=clock_timestamp()
  WHERE tenant_id=b.tenant_id AND run_id=p_run;
 UPDATE photo.runs SET status='failed',graph_sealed=true,revision=revision+1 WHERE tenant_id=b.tenant_id AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.fail_apply_build(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.fail_apply_build(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.fail_apply_build(uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
