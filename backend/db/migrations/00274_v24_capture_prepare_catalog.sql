-- +goose Up
-- Internal preparation node; admission remains gated until its expansion
-- and adoption are complete. It never replaces an original asset version.
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES('capture.prepare','1','gpu','gpu.capture_prepare',NULL,600,'["cuda","cpu"]',false);
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
VALUES
 ('capture.prepare','1','document','["application/json"]',8388608,1),
 ('capture.prepare','1','faces','["application/json"]',8388608,1),
 ('capture.prepare','1','working_image','["application/json"]',1048576,1),
 ('capture.prepare','1','working_rgb','["application/octet-stream"]',50339840,1),
 ('capture.prepare','1','working_alpha','["application/octet-stream"]',8396800,1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
