-- +goose Up
-- Catalog only; runtime qualification, graph admission and activation separate.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled) VALUES
('portrait.hair','1','cpu','cpu.portrait_hair',NULL,600,'["cpu"]',false),
('portrait.clothing','1','cpu','cpu.portrait_clothing',NULL,600,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('portrait.hair','1','document','["application/json"]',8388608,1),
('portrait.hair','1','working_image','["application/json"]',8388608,1),
('portrait.hair','1','pixels','["application/x-npy"]',2400004096,1),
('portrait.hair','1','report','["application/json"]',8388608,1),
('portrait.hair','1','mask','["application/json"]',8388608,2),
('portrait.hair','1','mask_pixels','["application/x-npy"]',800004096,2),
('portrait.hair','1','transform','["application/json"]',8388608,1),
('portrait.hair','1','registration','["application/json"]',8388608,30),
('portrait.hair','1','alpha','["application/x-npy"]',800004096,1),
('portrait.hair','1','warp_field','["application/x-npy"]',1600004096,32),
('portrait.clothing','1','document','["application/json"]',8388608,1),
('portrait.clothing','1','working_image','["application/json"]',8388608,1),
('portrait.clothing','1','pixels','["application/x-npy"]',2400004096,1),
('portrait.clothing','1','report','["application/json"]',8388608,1),
('portrait.clothing','1','mask','["application/json"]',8388608,2),
('portrait.clothing','1','mask_pixels','["application/x-npy"]',800004096,2),
('portrait.clothing','1','transform','["application/json"]',8388608,1),
('portrait.clothing','1','registration','["application/json"]',8388608,30),
('portrait.clothing','1','alpha','["application/x-npy"]',800004096,1),
('portrait.clothing','1','warp_field','["application/x-npy"]',1600004096,32);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
