-- V22 operation policies. Keep v2_enabled=false until the complete session
-- compiler, consent path, adoption and model qualification are installed.
INSERT INTO photo.operation_registry
 (operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
VALUES
 ('session.describe','1','gpu','gpu.session_describe',NULL,1800,'["cuda", "mps", "cpu"]',false),
 ('session.plan','1','cpu','cpu.session_plan',NULL,600,'["cpu"]',false);

INSERT INTO photo.operation_output_policies
 (operation,version,role,media_types,max_bytes,max_count)
VALUES
 ('session.describe','1','document','["application/json"]',8388608,1),
 ('session.describe','1','faces','["application/json"]',8388608,128),
 ('session.plan','1','document','["application/json"]',8388608,1),
 ('session.plan','1','correction_plan','["application/json"]',8388608,5000);
