-- +goose Up
-- GPU admission budget counts all outstanding GPU dispatches, including legacy.
-- The single capacity leader holds this predicate through reservation commit.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_heavy_admission_available(p_sha text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE policy text; ceiling bigint; outstanding bigint;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_locks WHERE locktype='advisory'
  AND pid=pg_backend_pid() AND granted AND classid=2026::oid AND objid=32::oid AND objsubid=2
  AND mode='ExclusiveLock' AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())) THEN
  RAISE EXCEPTION 'CAPACITY_LEADER_REQUIRED' USING ERRCODE='55000'; END IF;
 SELECT policy_jcs INTO policy FROM photo.capacity_policy_timings WHERE policy_sha=p_sha FOR SHARE;
 IF policy IS NULL OR NOT photo.capacity_policy_queue_valid(policy::jsonb) THEN RETURN false;END IF;
 ceiling:=(policy::jsonb->>'heavy_admission_inflight')::bigint;
 SELECT count(*) INTO outstanding FROM photo.tasks WHERE resource_class='gpu' AND state IN('queued','running');
 RETURN outstanding<ceiling;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_heavy_admission_available(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_heavy_admission_available(text) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_heavy_admission_available(text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
