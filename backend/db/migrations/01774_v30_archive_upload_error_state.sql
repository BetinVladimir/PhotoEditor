-- Archive failures are terminal cleanup states. Keep the ledger invariant
-- authoritative even if a future writer bypasses the status readers.
-- +goose Up
ALTER TABLE photo.v30_archive_uploads
 ADD CONSTRAINT v30_archive_upload_error_state_check
 CHECK ((state IN ('aborting','aborted')) = (error_code IS NOT NULL));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
