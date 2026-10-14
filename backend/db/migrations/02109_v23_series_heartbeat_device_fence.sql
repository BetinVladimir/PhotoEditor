-- +goose Up
-- Stop an already-claimed qualified series task at the next heartbeat after
-- its operator-provisioned worker/device binding is revoked or rotated.
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)
 RENAME TO heartbeat_task_pre_v23_series_device;
REVOKE ALL ON FUNCTION photo.heartbeat_task_pre_v23_series_device(
 uuid,integer,bigint,uuid,jsonb) FROM PUBLIC,photo_worker;
-- +goose StatementBegin
CREATE FUNCTION photo.heartbeat_task(
 p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_progress jsonb)
RETURNS TABLE(accepted boolean,lease_until timestamptz,cancel_requested boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE;
BEGIN
 t:=photo.lock_task(p_task);
 IF t.id IS NOT NULL AND t.operation='series.balance' AND
  t.execution_profile->>'device_profile_sha' IS NOT NULL AND
  (t.worker_id IS DISTINCT FROM p_worker OR
   t.attempt IS DISTINCT FROM p_attempt OR
   t.fencing_token IS DISTINCT FROM p_fence OR
   photo.v23_series_worker_device_matches(t.tenant_id,p_worker,
    t.execution_profile) IS DISTINCT FROM true) THEN
  RETURN QUERY SELECT false,NULL::timestamptz,false;
  RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.heartbeat_task_pre_v23_series_device(
  p_task,p_attempt,p_fence,p_worker,p_progress);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)
 TO photo_worker;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
