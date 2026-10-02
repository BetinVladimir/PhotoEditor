-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM photo.tasks WHERE operation='storage.cleanup' AND state NOT IN ('succeeded','failed','cancelled','skipped')) THEN
    RAISE EXCEPTION 'Cannot remove storage.cleanup registry entry while tasks are active';
  END IF;
  DELETE FROM photo.operation_output_policies WHERE operation='storage.cleanup' AND version='1';
  DELETE FROM photo.operation_registry WHERE operation='storage.cleanup' AND version='1';
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
