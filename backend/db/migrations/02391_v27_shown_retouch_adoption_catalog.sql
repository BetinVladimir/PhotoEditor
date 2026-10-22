-- +goose Up
-- Copy only the exact selected preview pixels. Keep disabled until receipt,
-- closure, worker lease authority and atomic Apply admission are installed.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('semantic.adopt_shown_retouch_region','1','cpu','cpu.semantic_adopt_shown_retouch_region',NULL,1800,'["cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
('semantic.adopt_shown_retouch_region','1','working_image','["application/json"]',1048576,1),
('semantic.adopt_shown_retouch_region','1','pixels','["application/x-npy"]',2400016384,1);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
