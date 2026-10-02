-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('derivative.resize','1','working_rgb','["application/octet-stream"]',2400004096,1),
 ('derivative.resize','1','working_alpha','["application/octet-stream"]',400004096,1),
 ('derivative.resize','1','working_image','["application/json"]',1048576,1),
 ('derivative.resize','1','derivative_report','["application/json"]',1048576,1),
 ('derivative.sharpen','1','working_rgb','["application/octet-stream"]',2400004096,1),
 ('derivative.sharpen','1','working_alpha','["application/octet-stream"]',400004096,1),
 ('derivative.sharpen','1','working_image','["application/json"]',1048576,1),
 ('derivative.sharpen','1','derivative_report','["application/json"]',1048576,1),
 ('derivative.watermark','1','working_rgb','["application/octet-stream"]',2400004096,1),
 ('derivative.watermark','1','working_alpha','["application/octet-stream"]',400004096,1),
 ('derivative.watermark','1','working_image','["application/json"]',1048576,1),
 ('derivative.watermark','1','derivative_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
