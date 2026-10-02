-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('scene.parse','1','scene_labels','["application/octet-stream"]',400004096,1),
 ('scene.parse','1','scene_labels_ref','["application/json"]',1048576,1),
 ('alpha.predict','1','alpha_mask','["application/octet-stream"]',800004096,1),
 ('alpha.predict','1','alpha_mask_png','["image/png"]',500000000,1),
 ('alpha.predict','1','alpha_mask_ref','["application/json"]',1048576,1),
 ('alpha.predict','1','alpha_report','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
