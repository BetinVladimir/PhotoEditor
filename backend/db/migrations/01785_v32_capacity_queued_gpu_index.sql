-- +goose NO TRANSACTION
-- +goose Up
-- Forward-only concurrent build: grouped tenant heavy counts and the scoped
-- affinity count read only queued GPU index entries.
CREATE INDEX CONCURRENTLY capacity_queued_gpu_tenant_idx
 ON photo.tasks(tenant_id) WHERE state='queued' AND resource_class='gpu';
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
