-- +goose Up
-- Closed declaration only: no operator activation, qualification or model installation.
-- +goose StatementBegin
DO $migration$
DECLARE expected jsonb:=$catalog$[{"operation":"portrait.glare","version":"1","resource_class":"gpu","routing_key":"gpu.portrait_glare","model_id":null,"timeout_seconds":1800,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"working_image","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"pixels","media_types":["application/x-npy"],"max_bytes":2400004096,"max_count":1},{"role":"report","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"mask","media_types":["application/json"],"max_bytes":8388608,"max_count":2},{"role":"mask_pixels","media_types":["application/x-npy"],"max_bytes":800004096,"max_count":2},{"role":"transform","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"registration","media_types":["application/json"],"max_bytes":8388608,"max_count":30},{"role":"alpha","media_types":["application/x-npy"],"max_bytes":800004096,"max_count":1},{"role":"warp_field","media_types":["application/x-npy"],"max_bytes":1600004096,"max_count":32}]},{"operation":"image.reflection","version":"1","resource_class":"gpu","routing_key":"gpu.image_reflection","model_id":null,"timeout_seconds":1800,"backends":["cpu","cuda"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"working_image","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"pixels","media_types":["application/x-npy"],"max_bytes":2400004096,"max_count":1},{"role":"report","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"mask","media_types":["application/json"],"max_bytes":8388608,"max_count":2},{"role":"mask_pixels","media_types":["application/x-npy"],"max_bytes":800004096,"max_count":2},{"role":"transform","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"registration","media_types":["application/json"],"max_bytes":8388608,"max_count":30},{"role":"alpha","media_types":["application/x-npy"],"max_bytes":800004096,"max_count":1},{"role":"warp_field","media_types":["application/x-npy"],"max_bytes":1600004096,"max_count":32}]},{"operation":"optical.perspective","version":"1","resource_class":"cpu","routing_key":"cpu.optical_perspective","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"working_image","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"pixels","media_types":["application/x-npy"],"max_bytes":2400004096,"max_count":1},{"role":"report","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"mask","media_types":["application/json"],"max_bytes":8388608,"max_count":2},{"role":"mask_pixels","media_types":["application/x-npy"],"max_bytes":800004096,"max_count":2},{"role":"transform","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"registration","media_types":["application/json"],"max_bytes":8388608,"max_count":30},{"role":"alpha","media_types":["application/x-npy"],"max_bytes":800004096,"max_count":1},{"role":"warp_field","media_types":["application/x-npy"],"max_bytes":1600004096,"max_count":32}]},{"operation":"provenance.sign","version":"1","resource_class":"cpu","routing_key":"cpu.provenance_sign","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"signed_export","media_types":["image/jpeg","image/png"],"max_bytes":2400004096,"max_count":1},{"role":"manifest","media_types":["application/c2pa"],"max_bytes":8388608,"max_count":1}]},{"operation":"v2.delivery.assemble","version":"1","resource_class":"cpu","routing_key":"cpu.v2_delivery_assemble","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1}]},{"operation":"v2.interchange.import","version":"1","resource_class":"cpu","routing_key":"cpu.v2_interchange_import","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"report","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"mapping_shard","media_types":["application/json"],"max_bytes":8388608,"max_count":50},{"role":"draft","media_types":["application/json"],"max_bytes":8388608,"max_count":5000}]},{"operation":"v2.interchange.export","version":"1","resource_class":"cpu","routing_key":"cpu.v2_interchange_export","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"report","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"mapping_shard","media_types":["application/json"],"max_bytes":8388608,"max_count":50},{"role":"draft","media_types":["application/json"],"max_bytes":8388608,"max_count":5000},{"role":"bundle","media_types":["application/zip"],"max_bytes":68719476736,"max_count":1}]},{"operation":"v2.interchange.parse","version":"1","resource_class":"cpu","routing_key":"cpu.v2_interchange_parse","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1},{"role":"imported_file","media_types":["image/jpeg","image/png","image/tiff","application/octet-stream","application/json","application/rdf+xml","application/x-npy"],"max_bytes":68719476736,"max_count":30000}]},{"operation":"v2.delivery.prepare","version":"1","resource_class":"cpu","routing_key":"cpu.v2_delivery_prepare","model_id":null,"timeout_seconds":600,"backends":["cpu"],"v2_enabled":false,"policies":[{"role":"document","media_types":["application/json"],"max_bytes":8388608,"max_count":1}]}]$catalog$::jsonb;
 entry jsonb; policy jsonb; runtime text; table_name text; old_acl jsonb; new_acl jsonb;
BEGIN
 -- Serialize declaration against another privileged catalog writer.
 LOCK TABLE photo.operation_registry,photo.operation_output_policies IN SHARE ROW EXCLUSIVE MODE;
 SELECT jsonb_agg(jsonb_build_object('oid',c.oid,'owner',c.relowner,'acl',c.relacl,'rls',c.relrowsecurity,'force',c.relforcerowsecurity) ORDER BY c.oid) INTO old_acl FROM pg_class c
 WHERE c.oid IN('photo.operation_registry'::regclass,'photo.operation_output_policies'::regclass);
 FOREACH runtime IN ARRAY ARRAY['photo_api','photo_scheduler','photo_worker','photo_gateway','photo_portal','photo_maintenance'] LOOP
  FOREACH table_name IN ARRAY ARRAY['photo.operation_registry','photo.operation_output_policies'] LOOP
   IF has_table_privilege(runtime,table_name,'INSERT') OR has_any_column_privilege(runtime,table_name,'UPDATE')
     OR has_table_privilege(runtime,table_name,'DELETE') THEN
    RAISE EXCEPTION 'REGISTRY_NINE_RUNTIME_CATALOG_AUTHORITY';
   END IF;
  END LOOP;
 END LOOP;
 -- Reject even identical preexisting tuples: Goose applies this declaration once.
 -- No ON CONFLICT can conceal a weakened or previously activated contract.
 FOR entry IN SELECT value FROM jsonb_array_elements(expected) LOOP
  IF EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation=entry->>'operation' AND version=entry->>'version')
   OR EXISTS(SELECT 1 FROM photo.operation_registry WHERE routing_key=entry->>'routing_key') THEN
   RAISE EXCEPTION 'REGISTRY_NINE_COLLISION: %',entry->>'operation';
  END IF;
 END LOOP;
 FOR entry IN SELECT value FROM jsonb_array_elements(expected) LOOP
  INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends,v2_enabled)
  VALUES(entry->>'operation',entry->>'version',entry->>'resource_class',entry->>'routing_key',NULL,
    (entry->>'timeout_seconds')::integer,entry->'backends',false);
  FOR policy IN SELECT value FROM jsonb_array_elements(entry->'policies') LOOP
   INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count)
   VALUES(entry->>'operation',entry->>'version',policy->>'role',policy->'media_types',
     (policy->>'max_bytes')::bigint,(policy->>'max_count')::integer);
  END LOOP;
 END LOOP;
 SELECT jsonb_agg(jsonb_build_object('oid',c.oid,'owner',c.relowner,'acl',c.relacl,'rls',c.relrowsecurity,'force',c.relforcerowsecurity) ORDER BY c.oid) INTO new_acl FROM pg_class c
 WHERE c.oid IN('photo.operation_registry'::regclass,'photo.operation_output_policies'::regclass);
 IF new_acl IS DISTINCT FROM old_acl THEN RAISE EXCEPTION 'REGISTRY_NINE_TABLE_METADATA_CHANGED';END IF;
END $migration$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
