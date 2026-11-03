-- +goose Up
-- DS-08 / TK-107: photo.quota_reservations.holder_type and photo.artifacts.output_role are free text
-- with no CHECK constraint or enum (verified: pg_constraint, pg_type, and a grep of all migrations show
-- only query filters on output_role, never validation). The new holder types video_upload / video_job and
-- the video output roles are therefore accepted without a schema change. Reservation functions are not touched.
SELECT 1;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
