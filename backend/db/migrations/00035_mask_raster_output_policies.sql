-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('mask.rasterize','1','mask_target','["application/octet-stream"]',801048576,1),
 ('mask.rasterize','1','mask_protected','["application/octet-stream"]',801048576,1),
 ('mask.rasterize','1','mask_target_png','["image/png"]',613048576,1),
 ('mask.rasterize','1','mask_protected_png','["image/png"]',613048576,1),
 ('mask.rasterize','1','rasterized_mask','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
