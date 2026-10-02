-- Evaluation remains disabled for dispatch until its renderer, identity
-- verifier, closure and atomic barrier close are complete and qualified.
-- +goose Up
INSERT INTO photo.operation_registry
 (operation,version,resource_class,routing_key,model_id,timeout_seconds,
  backends,v2_enabled)
VALUES ('learning.evaluate','1','gpu','gpu.learning_evaluate',NULL,1800,
 '["cuda"]'::jsonb,false);

INSERT INTO photo.operation_output_policies
 (operation,version,role,media_types,max_bytes,max_count)
VALUES ('learning.evaluate','1','document','["application/json"]'::jsonb,8388608,1);
