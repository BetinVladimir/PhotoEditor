-- +goose Up
-- Register limits; qualification and activation remain separate required gates.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('region.preview','1','gpu','gpu.region_preview',NULL,1800,'["cuda"]'::jsonb,false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES ('region.preview','1','document','["application/json"]'::jsonb,8388608,1),
 ('region.preview','1','derivative','["image/jpeg","image/webp"]'::jsonb,16777216,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
