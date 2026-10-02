-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('background.sky','1','working_rgb','["application/octet-stream"]',2401048576,1),
 ('background.sky','1','working_image','["application/json"]',1048576,1),
 ('background.sky','1','sky_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
