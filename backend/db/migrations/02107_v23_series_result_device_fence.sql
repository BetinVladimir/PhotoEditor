-- Recheck the operator's worker/device binding in the canonical success
-- transaction. Revoking or rotating it after claim must not publish a result.
-- +goose Up
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 RENAME TO commit_task_success_pre_v23_series_device;
REVOKE ALL ON FUNCTION photo.commit_task_success_pre_v23_series_device(
 uuid,integer,bigint,uuid,text) FROM PUBLIC,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.commit_task_success(
 p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_manifest text)
RETURNS TABLE(outcome text,result_id uuid,result_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 t:=photo.lock_task(p_task);
 IF t.id IS NOT NULL AND t.operation='series.balance' AND
  t.execution_profile->>'device_profile_sha' IS NOT NULL AND
  t.state='running' THEN
  -- Serialize operator rotation with result publication. A concurrent
  -- UPDATE/DELETE of this registration completes before or after commit.
  PERFORM 1 FROM photo.worker_registrations w
   WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker FOR SHARE;
 END IF;
 IF t.id IS NOT NULL AND t.operation='series.balance' AND
  t.execution_profile->>'device_profile_sha' IS NOT NULL AND
  t.state='running' AND
  (t.worker_id IS DISTINCT FROM p_worker OR
   t.attempt IS DISTINCT FROM p_attempt OR
   t.fencing_token IS DISTINCT FROM p_fence OR
   photo.v23_series_worker_device_matches(t.tenant_id,p_worker,
    t.execution_profile) IS DISTINCT FROM true) THEN
  RETURN QUERY SELECT 'stale'::text,NULL::uuid,NULL::text;
  RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.commit_task_success_pre_v23_series_device(
  p_task,p_attempt,p_fence,p_worker,p_manifest);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.commit_task_success(uuid,integer,bigint,uuid,text)
 TO photo_worker;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
