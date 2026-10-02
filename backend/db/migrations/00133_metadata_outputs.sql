-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('metadata.write','1','encoded_image','["image/jpeg","image/png","image/tiff"]',1073741824,1),
 ('metadata.write','1','encoded_manifest','["application/json"]',1048576,1),
 ('metadata.write','1','metadata_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
