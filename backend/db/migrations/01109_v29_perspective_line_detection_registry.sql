-- +goose Up
-- Candidate extraction only. V2 dispatch remains disabled until source/closure
-- authority, result adoption and trusted runtime installation are complete.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('optical.perspective_lines','1','cpu','cpu.perspective_lines',NULL,600,'["cpu"]'::jsonb,false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES('optical.perspective_lines','1','document','["application/json"]'::jsonb,1048576,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
