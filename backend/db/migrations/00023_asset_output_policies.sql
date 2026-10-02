-- +goose Up
-- Only executable asset adapters are enabled here; other operations stay deny.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('asset.verify','1','verified','["application/json"]',1048576,1),
 ('asset.inspect','1','metadata','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
