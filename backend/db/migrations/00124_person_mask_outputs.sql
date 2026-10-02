-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('mask.person','1','person_mask','["application/octet-stream"]',801048576,1),
 ('mask.person','1','person_mask_png','["image/png"]',613048576,1),
 ('mask.person','1','person_mask_ref','["application/json"]',1048576,1),
 ('mask.person','1','person_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
