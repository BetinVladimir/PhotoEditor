-- +goose Up
-- Worker-capable lens operation. Dispatch activation awaits complete compiler
-- admission, adoption and approved photographic qualification.
INSERT INTO photo.operation_registry
 (operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES ('optical.lens','1','cpu','cpu.optical_lens',NULL,600,'["cpu"]'::jsonb,false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES
 ('optical.lens','1','document','["application/json"]'::jsonb,8388608,1),
 ('optical.lens','1','working_image','["application/json"]'::jsonb,8388608,1),
 ('optical.lens','1','pixels','["application/x-npy"]'::jsonb,2400004096,1),
 ('optical.lens','1','report','["application/json"]'::jsonb,8388608,1),
 ('optical.lens','1','mask','["application/json"]'::jsonb,8388608,2),
 ('optical.lens','1','mask_pixels','["application/x-npy"]'::jsonb,800004096,2),
 ('optical.lens','1','transform','["application/json"]'::jsonb,8388608,1),
 ('optical.lens','1','registration','["application/json"]'::jsonb,8388608,30),
 ('optical.lens','1','alpha','["application/x-npy"]'::jsonb,800004096,1),
 ('optical.lens','1','warp_field','["application/x-npy"]'::jsonb,1600004096,32);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
