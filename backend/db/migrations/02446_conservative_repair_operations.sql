-- +goose Up
-- Automatic selection has separate authorization/contracts; reviewed repair
-- documents and their operation versions remain byte-stable.
-- +goose StatementBegin
DO $$
BEGIN
 IF (SELECT count(*) FROM photo.operation_registry
     WHERE (operation,version) IN (('defects.repair','1'),('sensor.dust_repair','1'))
       AND resource_class='cpu' AND backends ? 'cpu')<>2 THEN
  RAISE EXCEPTION 'Conservative repair requires installed CPU repair contracts';
 END IF;
 IF (SELECT count(*) FROM photo.operation_output_policies
     WHERE (operation,version) IN (('defects.repair','1'),('sensor.dust_repair','1'))
       AND role IN ('working_rgb','working_image','defect_repair_report','dust_repair_report'))<>6 THEN
  RAISE EXCEPTION 'Conservative repair requires complete output policies';
 END IF;
END $$;
-- +goose StatementEnd
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
SELECT CASE operation WHEN 'defects.repair' THEN 'defects.auto_repair' ELSE 'sensor.dust_auto_repair' END,
 version,'cpu',CASE operation WHEN 'defects.repair' THEN 'cpu.defects_auto_repair' ELSE 'cpu.dust_auto_repair' END,
 NULL,300,'["cpu"]'::jsonb
FROM photo.operation_registry WHERE (operation,version) IN (('defects.repair','1'),('sensor.dust_repair','1'));
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
SELECT CASE operation WHEN 'defects.repair' THEN 'defects.auto_repair' ELSE 'sensor.dust_auto_repair' END,
 version,role,media_types,max_bytes,max_count
FROM photo.operation_output_policies WHERE (operation,version) IN (('defects.repair','1'),('sensor.dust_repair','1'))
 AND role IN ('working_rgb','working_image','defect_repair_report','dust_repair_report');
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
