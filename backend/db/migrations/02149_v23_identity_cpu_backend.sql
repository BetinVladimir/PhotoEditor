-- +goose Up
-- The pinned ONNX identity512 adapter has exact CPU and CUDA providers.
-- CPU permits a qualified identity task on a Metal host without claiming a
-- nonexistent MPS ONNX provider. Dispatch remains disabled until full
-- source/consent/model admission and result validation are installed.
-- +goose StatementBegin
DO $$
DECLARE updated_count integer;
BEGIN
 UPDATE photo.operation_registry SET backends='["cuda","cpu"]'::jsonb
 WHERE operation='identity.compare' AND version='1'
  AND resource_class='gpu' AND routing_key='gpu.identity_compare'
  AND backends='["cuda"]'::jsonb AND v2_enabled=false;
 GET DIAGNOSTICS updated_count=ROW_COUNT;
 IF updated_count<>1 THEN RAISE EXCEPTION 'IDENTITY_CPU_CATALOG_SOURCE_SHAPE';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
