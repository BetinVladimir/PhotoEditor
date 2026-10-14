-- +goose Up
-- Standalone original target locator; producer/root/pixel verification is still
-- required for adoption. Empty protection behavior is unchanged.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
 VALUES('mask.rasterize','1','mask_target_ref','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
