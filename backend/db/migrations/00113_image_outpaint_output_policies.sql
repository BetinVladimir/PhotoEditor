-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('image.outpaint','1','working_rgb','["application/octet-stream"]',2401048576,1),
 ('image.outpaint','1','working_alpha','["application/octet-stream"]',400104857,1),
 ('image.outpaint','1','working_image','["application/json"]',1048576,1),
 ('image.outpaint','1','generation_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
