-- Publish an exact, nonempty protected MaskRef alongside mask.rasterize's
-- existing RasterizedMask result. Older tasks and outputs remain valid.
-- +goose Up
INSERT INTO photo.operation_output_policies
 (operation,version,role,media_types,max_bytes,max_count)
VALUES ('mask.rasterize','1','mask_protected_ref',
 '["application/json"]'::jsonb,1048576,1);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
