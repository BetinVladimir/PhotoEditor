-- +goose Up
-- Preserve historical request hashes and keys while durably binding path parents.
-- NULL denotes a legacy receipt: API must prove its immutable resource binding
-- before adopting it; absence of proof never permits a second admission.
ALTER TABLE photo.idempotency_records ADD COLUMN parent_binding text;
ALTER TABLE photo.idempotency_records ADD CONSTRAINT idempotency_parent_binding_bounded
 CHECK (parent_binding IS NULL OR length(parent_binding) BETWEEN 1 AND 256);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
