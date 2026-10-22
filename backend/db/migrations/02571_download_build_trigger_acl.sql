-- +goose Up
-- Trigger execution does not require a public callable entry point.
-- Retain the existing trigger/function identity, owner and implementation.
REVOKE ALL ON FUNCTION photo.init_download_build_state() FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
