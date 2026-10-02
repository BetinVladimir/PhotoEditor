-- +goose Up
-- Apply admission inserts the build row in the same API transaction as the
-- immutable run, selection snapshot and quota reservation. The original
-- build table granted only function-owner access, so real Apply returned 42501.
CREATE POLICY api_admission_insert ON photo.apply_builds FOR INSERT TO photo_api
 WITH CHECK (tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
GRANT INSERT ON photo.apply_builds TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
