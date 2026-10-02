-- +goose Up
-- NPY float32 HWC RGB and uint16 HW alpha, at most 200 million pixels.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('asset.decode','1','working_rgb','["application/octet-stream"]',2401048576,1),
 ('asset.decode','1','alpha','["application/octet-stream"]',401048576,1),
 ('asset.decode','1','working_image','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
