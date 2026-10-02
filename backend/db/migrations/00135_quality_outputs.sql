-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('quality.check','1','quality_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
