-- +goose Up
-- Retry selection needs only scoped identity, ordering and the retryability envelope.
-- Existing forced RLS tenant_scope remains authoritative for photo_api.
GRANT SELECT (tenant_id, task_id, attempt, error) ON photo.task_attempts TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
