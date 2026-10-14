-- +goose Up
-- Catalog only. Runtime qualification and deployment activation are separate.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('semantic.local_exposure','1','cpu','cpu.semantic_local_exposure',NULL,1800,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.local_exposure','1','document','["application/json"]',8388608,1),
('semantic.local_exposure','1','working_image','["application/json"]',8388608,1),
('semantic.local_exposure','1','pixels','["application/x-npy"]',2400016384,1),
('semantic.local_exposure','1','report','["application/json"]',8388608,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
