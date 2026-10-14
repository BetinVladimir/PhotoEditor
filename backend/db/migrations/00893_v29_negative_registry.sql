-- +goose Up
-- Catalog and output bounds only. Runtime qualification and public graph
-- activation are separate gates; keep v2_enabled false until verified.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled) VALUES
('optical.negative','1','cpu','cpu.optical_negative',NULL,600,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('optical.negative','1','document','["application/json"]',8388608,1),
('optical.negative','1','working_image','["application/json"]',8388608,1),
('optical.negative','1','pixels','["application/x-npy"]',2400004096,1),
('optical.negative','1','report','["application/json"]',8388608,1),
('optical.negative','1','mask','["application/json"]',8388608,2),
('optical.negative','1','mask_pixels','["application/x-npy"]',800004096,2),
('optical.negative','1','transform','["application/json"]',8388608,1),
('optical.negative','1','registration','["application/json"]',8388608,30),
('optical.negative','1','alpha','["application/x-npy"]',800004096,1),
('optical.negative','1','warp_field','["application/x-npy"]',1600004096,32);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
