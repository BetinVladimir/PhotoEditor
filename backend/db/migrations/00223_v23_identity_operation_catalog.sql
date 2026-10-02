-- The identity primitive is GPU-scoped. Dispatch remains disabled until its
-- qualified detector/embedding adapter and output validation are complete.
-- +goose Up
INSERT INTO photo.operation_registry
 (operation,version,resource_class,routing_key,model_id,timeout_seconds,
  backends,v2_enabled)
VALUES ('identity.compare','1','gpu','gpu.identity_compare',NULL,1800,
 '["cuda"]'::jsonb,false);

INSERT INTO photo.operation_output_policies
 (operation,version,role,media_types,max_bytes,max_count)
VALUES
 ('identity.compare','1','document','["application/json"]'::jsonb,8388608,1),
 ('identity.compare','1','mask','["application/json"]'::jsonb,8388608,1),
 ('identity.compare','1','mask_pixels','["application/x-npy"]'::jsonb,800004096,1);
