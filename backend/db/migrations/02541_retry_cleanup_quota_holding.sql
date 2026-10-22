-- +goose Up
-- Retain existing capacity while unknown physical IO waits its quiet period.
-- The task deadline is per attempt and cannot bound cleanup after a timed-out attempt.
-- +goose StatementBegin
CREATE FUNCTION photo.renew_next_retry_cleanup_quota() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 qid uuid;bound timestamptz;until_time timestamptz;
BEGIN
 SELECT task.id AS task_id,quota.id AS quota_id INTO candidate FROM photo.tasks task JOIN photo.runs run ON run.tenant_id=task.tenant_id AND run.id=task.run_id
 JOIN photo.quota_reservations quota ON quota.tenant_id=task.tenant_id AND quota.holder_type='run' AND quota.holder_id=task.run_id
 WHERE task.state='retry_wait' AND NOT task.cancel_requested AND NOT run.cancel_requested
 AND run.status IN('queued','running','waiting_review') AND quota.state='reserved'
 AND quota.expires_at>clock_timestamp() AND quota.expires_at<clock_timestamp()+interval '5 minutes'
 AND quota.principal_type='user' AND quota.principal_id=run.created_by AND quota.share_id IS NULL AND quota.files=0
 AND EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=task.tenant_id AND tenant.status='active')
 AND EXISTS(SELECT 1 FROM photo.output_uploads allocation WHERE allocation.tenant_id=task.tenant_id
  AND allocation.task_id=task.id AND allocation.run_reservation_id=quota.id AND NOT allocation.returned_to_run_reservation)
 AND quota.expires_at<(SELECT max(allocation.created_at)+interval '4 hours' FROM photo.output_uploads allocation
  WHERE allocation.tenant_id=task.tenant_id AND allocation.run_reservation_id=quota.id AND NOT allocation.returned_to_run_reservation)
 ORDER BY quota.expires_at,task.id LIMIT 1;
 IF candidate.task_id IS NULL THEN RETURN false;END IF;
 t:=photo.lock_task(candidate.task_id);
 SELECT * INTO r FROM photo.runs WHERE tenant_id=t.tenant_id AND id=t.run_id;
 qid:=candidate.quota_id;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=t.tenant_id AND id=qid FOR UPDATE;
 IF t.state<>'retry_wait' OR t.cancel_requested OR r.cancel_requested OR r.status NOT IN('queued','running','waiting_review')
 OR q.id IS NULL OR q.state<>'reserved' OR q.expires_at<=clock_timestamp()
 OR q.holder_type<>'run' OR q.holder_id<>r.id OR q.principal_type<>'user' OR q.principal_id<>r.created_by
 OR q.share_id IS NOT NULL OR q.files<>0 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=t.tenant_id AND status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.output_uploads allocation WHERE allocation.tenant_id=t.tenant_id AND allocation.task_id=t.id
  AND allocation.run_reservation_id=q.id AND NOT allocation.returned_to_run_reservation) THEN RETURN false;END IF;
 SELECT max(allocation.created_at)+interval '4 hours' INTO bound FROM photo.output_uploads allocation
 WHERE allocation.tenant_id=t.tenant_id AND allocation.run_reservation_id=q.id AND NOT allocation.returned_to_run_reservation;
 until_time:=least(bound,clock_timestamp()+interval '15 minutes');
 IF bound IS NULL OR q.expires_at<=clock_timestamp() OR until_time<=clock_timestamp() OR until_time<=q.expires_at THEN RETURN false;END IF;
 -- 02541: exact already-held reservation only; no bytes or usage changes, no expired revival.
 UPDATE photo.quota_reservations SET expires_at=until_time WHERE tenant_id=q.tenant_id AND id=q.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.renew_next_retry_cleanup_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_next_retry_cleanup_quota() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_maintenance,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.renew_next_retry_cleanup_quota() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
