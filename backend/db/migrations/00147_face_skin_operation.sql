-- Materialize conservative skin masks from the pinned Face19 semantic result.
-- This closes the explicit artifact boundary required by pair effect fitting.

-- +goose Up
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
VALUES('mask.face_skin','1','cpu','cpu.mask',NULL,180,'["cpu"]')
ON CONFLICT(operation,version) DO UPDATE SET
 resource_class=EXCLUDED.resource_class,routing_key=EXCLUDED.routing_key,model_id=EXCLUDED.model_id,
 timeout_seconds=EXCLUDED.timeout_seconds,backends=EXCLUDED.backends;

INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('mask.face_skin','1','skin_mask','["application/octet-stream"]',800004096,1),
 ('mask.face_skin','1','skin_mask_png','["image/png"]',400004096,1),
 ('mask.face_skin','1','skin_mask_ref','["application/json"]',1048576,1);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
