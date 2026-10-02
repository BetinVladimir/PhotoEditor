-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('media.thumbnail','1','derivative','["image/jpeg","image/webp"]',25165824,1),
 ('media.thumbnail','1','derivative_manifest','["application/json"]',1048576,1),
 ('media.preview','1','derivative','["image/jpeg","image/webp"]',50331648,1),
 ('media.preview','1','derivative_manifest','["application/json"]',1048576,1);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
