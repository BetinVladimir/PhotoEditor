-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('style.resolve','1','resolved_style','["application/json"]',1048576,1),
 ('style.resolve','1','absolute_lut','["application/octet-stream"]',3303692,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
