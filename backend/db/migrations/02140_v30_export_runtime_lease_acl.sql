-- +goose Up
-- The global process lease needs read/announce/withdraw only. The table owner
-- inherited DELETE and other default DML privileges at creation; remove them
-- so the function owner has no direct purge path through this table.
REVOKE ALL ON photo.v30_export_runtime_leases FROM photo_fn_owner;
GRANT SELECT,INSERT,UPDATE ON photo.v30_export_runtime_leases TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
