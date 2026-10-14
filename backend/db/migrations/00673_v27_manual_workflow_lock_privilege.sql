-- +goose Up
-- PostgreSQL row-lock clauses require UPDATE privilege on at least one column.
-- Trusted NOLOGIN function owner only; no API/scheduler direct UPDATE grant.
GRANT UPDATE(run_id) ON photo.v2_workflows TO photo_fn_owner;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
