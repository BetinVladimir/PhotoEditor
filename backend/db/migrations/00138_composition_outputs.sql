-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('composition.plan','1','composition_plan','["application/json"]',1048576,1),
 ('geometry.crop','1','working_rgb','["application/octet-stream"]',2400004096,1),
 ('geometry.crop','1','working_alpha','["application/octet-stream"]',400004096,1),
 ('geometry.crop','1','working_image','["application/json"]',1048576,1),
 ('geometry.crop','1','crop_report','["application/json"]',1048576,1);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
