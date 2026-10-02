-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.capture_operations_ready(p_backend text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_backend IN('cpu','cuda') AND count(*)=2 FROM photo.operation_registry
 WHERE operation IN('capture.prepare','capture.assess') AND version='1'
 AND v2_enabled AND backends ? p_backend;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_operations_ready(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_operations_ready(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_operations_ready(text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
