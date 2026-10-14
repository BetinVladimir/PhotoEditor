-- +goose Up
-- MPS executes the same qualified model-lock path as CPU/CUDA. Merely adding
-- the backend does not enable either operation or install qualified weights.
-- +goose StatementBegin
DO $$
DECLARE updated_count integer;
BEGIN
 UPDATE photo.operation_registry SET backends='["cuda","cpu","mps"]'::jsonb
 WHERE version='1' AND operation IN('capture.prepare','capture.assess')
  AND backends='["cuda","cpu"]'::jsonb;
 GET DIAGNOSTICS updated_count=ROW_COUNT;
 IF updated_count<>2 THEN RAISE EXCEPTION 'CAPTURE_MPS_CATALOG_SOURCE_SHAPE';END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.capture_operations_ready(p_backend text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_backend IN('cpu','cuda','mps') AND count(*)=2 FROM photo.operation_registry
 WHERE operation IN('capture.prepare','capture.assess') AND version='1'
 AND v2_enabled AND backends ? p_backend;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_operations_ready(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_operations_ready(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_operations_ready(text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
