-- +goose Up
-- Runtime catalog registration only. Qualified deployment activation is separate.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('semantic.transfer','1','gpu','gpu.semantic_transfer',NULL,1800,'["cuda"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.transfer','1','document','["application/json"]',8388608,1),
('semantic.transfer','1','mask','["application/json"]',8388608,1),
('semantic.transfer','1','mask_pixels','["application/x-npy"]',800004096,1),
('semantic.transfer','1','transform','["application/json"]',8388608,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
