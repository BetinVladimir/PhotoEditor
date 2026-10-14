-- +goose NO TRANSACTION
-- +goose Up
-- Forward-only concurrent build: tenant-scoped FIFO heads can scan ready rows
-- in their immutable creation order without sorting a tenant burst.
CREATE INDEX CONCURRENTLY capacity_ready_tenant_created_idx
 ON photo.tasks(tenant_id,created_at,id) WHERE state='ready';
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
