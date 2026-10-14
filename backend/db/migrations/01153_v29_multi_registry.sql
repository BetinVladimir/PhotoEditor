-- +goose Up
-- Catalogue/output bounds only; trusted qualification and runtime evidence are
-- separate deployment gates. Neither operation is enabled by this migration.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled) VALUES
('optical.panorama','1','cpu','cpu.optical_panorama',NULL,600,'["cpu"]'::jsonb,false),
('optical.focus_stack','1','cpu','cpu.optical_focus_stack',NULL,600,'["cpu"]'::jsonb,false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
SELECT operation,'1',role,media_types,max_bytes,
 CASE role WHEN 'registration' THEN frame_limit WHEN 'warp_field' THEN frame_limit ELSE 1 END
FROM (VALUES('optical.panorama',20),('optical.focus_stack',30)) ops(operation,frame_limit)
CROSS JOIN (VALUES
 ('document','["application/json"]'::jsonb,8388608::bigint),
 ('working_image','["application/json"]'::jsonb,8388608::bigint),
 ('pixels','["application/x-npy"]'::jsonb,2400004096::bigint),
 ('alpha','["application/x-npy"]'::jsonb,800004096::bigint),
 ('mask_pixels','["application/x-npy"]'::jsonb,800004096::bigint),
 ('mask','["application/json"]'::jsonb,8388608::bigint),
 ('report','["application/json"]'::jsonb,8388608::bigint),
 ('registration','["application/json"]'::jsonb,8388608::bigint),
 ('warp_field','["application/x-npy"]'::jsonb,1600004096::bigint)
) bounds(role,media_types,max_bytes);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
