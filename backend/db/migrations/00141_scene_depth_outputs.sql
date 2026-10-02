-- +goose Up
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('features.scene','1','scene_embedding','["application/json"]',1048576,1),
 ('depth.predict','1','relative_depth','["application/octet-stream"]',800004096,1),
 ('depth.predict','1','depth_invalid','["application/octet-stream"]',800004096,1),
 ('depth.predict','1','relative_depth_ref','["application/json"]',1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
