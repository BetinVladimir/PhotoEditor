-- +goose Up
ALTER TABLE photo.training_pairs ADD COLUMN source_kind text NOT NULL DEFAULT 'explicit_pair';
ALTER TABLE photo.training_pairs ADD CONSTRAINT training_pairs_source_kind CHECK(source_kind IN ('explicit_pair','feedback_edit','user_added_generated'));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
