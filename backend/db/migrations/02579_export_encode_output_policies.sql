-- +goose Up
-- The encoder stages exactly these two outputs. Match the existing metadata
-- encoder limits without granting any new table or function privilege.
-- +goose StatementBegin
DO $$ BEGIN
 IF NOT EXISTS(SELECT FROM photo.operation_registry WHERE operation='export.encode' AND version='1')
 OR EXISTS(SELECT FROM photo.operation_output_policies WHERE operation='export.encode' AND version='1') THEN
  RAISE EXCEPTION 'EXPORT_ENCODE_OUTPUT_POLICY_SOURCE_SHAPE';END IF;
 INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('export.encode','1','encoded_image','["image/jpeg","image/png","image/tiff"]',1073741824,1),
 ('export.encode','1','encoded_manifest','["application/json"]',1048576,1);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
