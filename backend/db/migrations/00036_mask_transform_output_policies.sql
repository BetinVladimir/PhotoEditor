-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('mask.transform','1','transformed_mask','["application/octet-stream"]',801048576,1),
 ('mask.transform','1','transformed_mask_png','["image/png"]',613048576,1),
 ('mask.transform','1','mask_transform_result','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
