-- +goose Up
-- A physical purge marker belongs only to an aborted generation. In
-- particular, a still-running Create must never carry terminal purge evidence.
-- NOT VALID retains any pre-existing inconsistent row for explicit repair;
-- PostgreSQL enforces the constraint on every new insert or update.
ALTER TABLE photo.v30_archive_begin_generations
 ADD CONSTRAINT v30_archive_purge_marker_requires_aborted
 CHECK (purged_at IS NULL OR state='aborted') NOT VALID;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
