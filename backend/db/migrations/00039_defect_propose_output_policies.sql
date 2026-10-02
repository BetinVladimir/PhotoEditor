-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('defects.propose','1','defect_labels','["application/octet-stream"]',401048576,1),
 ('defects.propose','1','defect_protection','["application/octet-stream"]',801048576,1),
 ('defects.propose','1','defect_labels_png','["image/png"]',613048576,1),
 ('defects.propose','1','defect_candidates','["application/json"]',1048576,1),
 ('defects.propose','1','defect_propose_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
