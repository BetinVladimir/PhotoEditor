-- +goose Up
-- Presentation staging policies only; deliberately does not enable dispatch.
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.preview_local_exposure','1','preview_presentation','["application/json"]',1048576,1),
('semantic.preview_local_exposure','1','before','["image/png"]',33882112,1),
('semantic.preview_local_exposure','1','after','["image/png"]',33882112,1),
('semantic.preview_local_exposure','1','affected','["image/png"]',32833536,1),
('semantic.preview_local_exposure','1','protected','["image/png"]',32833536,1);
