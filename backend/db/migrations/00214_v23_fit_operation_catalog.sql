-- Register the worker-capable fit operation. V2 admission stays disabled
-- until its exact task closure, evaluate barrier and adoption are installed.
-- +goose Up
INSERT INTO photo.operation_registry
 (operation,version,resource_class,routing_key,model_id,timeout_seconds,
  backends,v2_enabled)
VALUES ('learning.fit','1','cpu','cpu.learning_fit',NULL,600,
 '["cpu"]'::jsonb,false);

INSERT INTO photo.operation_output_policies
 (operation,version,role,media_types,max_bytes,max_count)
VALUES ('learning.fit','1','document','["application/json"]'::jsonb,8388608,1);
