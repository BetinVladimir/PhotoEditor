-- +goose Up
-- 2048 x 2048 float32 alpha plus the existing 8192-byte NPY allowance.
-- uint16 remains accepted; provenance and shape validation are unchanged.
UPDATE photo.operation_output_policies SET max_bytes=16785408
 WHERE operation='capture.prepare' AND version='1' AND role='working_alpha';
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
