-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('geometry.body','1','inverse_field','["application/octet-stream"]',1600004096,1),
 ('geometry.body','1','working_rgb','["application/octet-stream"]',2400004096,1),
 ('geometry.body','1','working_alpha','["application/octet-stream"]',400004096,1),
 ('geometry.body','1','working_image','["application/json"]',1048576,1),
 ('geometry.body','1','body_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
