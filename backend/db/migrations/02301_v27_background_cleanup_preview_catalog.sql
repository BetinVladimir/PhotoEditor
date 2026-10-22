-- +goose Up
-- Internal proposal renderer catalog only. Disabled until independent preview
-- admission, complete input registration and output adoption are installed.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('semantic.preview_background_cleanup','1','gpu','gpu.semantic_preview_remove_object','big_lama',1800,'["cpu","cuda","mps"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.preview_background_cleanup','1','preview_evidence','["application/json"]',1048576,1),
('semantic.preview_background_cleanup','1','working_image','["application/json"]',1048576,1),
('semantic.preview_background_cleanup','1','pixels','["application/x-npy"]',2400016384,1);

-- Presentation staging policies only; deliberately does not enable dispatch.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.preview_background_cleanup','1','preview_presentation','["application/json"]',1048576,1),
('semantic.preview_background_cleanup','1','before','["image/png"]',33882112,1),
('semantic.preview_background_cleanup','1','after','["image/png"]',33882112,1),
('semantic.preview_background_cleanup','1','affected','["image/png"]',32833536,1),
('semantic.preview_background_cleanup','1','protected','["image/png"]',32833536,1);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
