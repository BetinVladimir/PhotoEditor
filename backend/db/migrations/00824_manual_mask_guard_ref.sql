-- +goose Up
-- A protection input may be explicitly empty. This role is not the existing
-- nonempty mask_protected_ref learning-evidence role and grants no authority.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
 VALUES('mask.rasterize','1','mask_guard_ref','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
