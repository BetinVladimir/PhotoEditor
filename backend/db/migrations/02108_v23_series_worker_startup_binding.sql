-- +goose Up
-- A worker may read, but never write, its operator-provisioned binding.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_worker_runtime_ready(
 p_worker uuid,p_runtime text,p_device text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.worker_registrations w
  WHERE w.worker_id=p_worker AND w.db_principal=session_user AND w.enabled
   AND w.resource_classes ? 'cpu' AND w.backends ? 'cpu'
   AND (w.qualified_operations->'series.balance') ? 'cpu'
   AND p_runtime ~ '^[0-9a-f]{64}$' AND p_device ~ '^[0-9a-f]{64}$'
   AND w.lock_id=p_runtime
   AND w.v23_series_device_profile_sha=p_device);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_worker_runtime_ready(uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_worker_runtime_ready(uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_series_worker_runtime_ready(uuid,text,text)
 TO photo_worker;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
