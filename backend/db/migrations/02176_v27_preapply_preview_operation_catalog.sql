-- +goose Up
-- Internal proposal renderer catalog only. Disabled until independent preview
-- admission, complete input registration and output adoption are installed.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('semantic.preview_local_exposure','1','cpu','cpu.semantic_preview_local_exposure',NULL,1800,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.preview_local_exposure','1','preview_evidence','["application/json"]',1048576,1),
('semantic.preview_local_exposure','1','working_image','["application/json"]',1048576,1),
('semantic.preview_local_exposure','1','pixels','["application/x-npy"]',2400016384,1);
