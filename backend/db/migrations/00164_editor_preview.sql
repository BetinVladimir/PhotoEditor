-- Executable, revision-bound editor preview jobs.
-- +goose Up
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
VALUES('editor.preview','1','media','io.preview',NULL,300,'["cpu"]');
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('editor.preview','1','derivative','["image/jpeg"]',50331648,1),
 ('editor.preview','1','derivative_manifest','["application/json"]',1048576,1);

ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_kind_check;
ALTER TABLE photo.interactive_tasks DROP CONSTRAINT interactive_tasks_check;
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_kind_check
 CHECK(kind IN('mask_manual','mask_prompt','generation','preview'));
ALTER TABLE photo.interactive_tasks ADD CONSTRAINT interactive_tasks_operation_check
 CHECK(operation=CASE kind WHEN 'mask_manual' THEN 'mask.rasterize' WHEN 'mask_prompt' THEN 'mask.prompt' WHEN 'preview' THEN 'editor.preview' ELSE 'image.generate' END);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
