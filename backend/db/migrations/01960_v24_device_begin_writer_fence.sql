-- +goose Up
-- A durable claim precedes the non-idempotent object-store Begin. An unknown
-- remote result must be reconciled explicitly; timeouts never clear this claim.
ALTER TABLE photo.upload_sessions ADD COLUMN device_begin_token uuid;
UPDATE photo.upload_sessions SET device_begin_token=gen_random_uuid()
 WHERE principal_type='device' AND state='initializing';
ALTER TABLE photo.upload_sessions ADD CONSTRAINT device_begin_token_scope
 CHECK (device_begin_token IS NULL OR principal_type='device');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
