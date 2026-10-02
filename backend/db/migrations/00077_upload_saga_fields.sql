-- +goose Up
ALTER TABLE photo.upload_sessions ADD COLUMN media_type text;
ALTER TABLE photo.upload_sessions ADD COLUMN expected_sha256 text;
ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_media_type_valid
 CHECK(media_type IS NULL OR media_type ~ '^[a-z0-9.+-]+/[a-z0-9.+-]+$');
ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_expected_sha_valid
 CHECK(expected_sha256 IS NULL OR expected_sha256 ~ '^[0-9a-f]{64}$');
ALTER TABLE photo.upload_sessions ADD CONSTRAINT upload_multipart_state CHECK(
 (state='initializing' AND multipart_id IS NULL) OR state<>'initializing');

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
