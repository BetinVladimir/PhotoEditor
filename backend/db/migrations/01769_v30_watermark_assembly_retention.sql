-- +goose Up
-- Retain watermark roots/arrays alongside the exact completed assembly closure.
-- Existing saved-assembly/producer gates establish current pin authority first.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;extra text;
BEGIN
 definition:=pg_get_functiondef('photo.retain_v30_delivery_assembly(uuid,uuid,uuid,text)'::regprocedure);
 needle:='UNION ALL SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=e.parent_task_id';
 extra:=$patch$
 UNION ALL SELECT (item->'ref'->>'artifact_id')::uuid,item->'ref'->>'sha256'
 FROM photo.runs parent CROSS JOIN LATERAL jsonb_each(parent.v30_watermark_sources) pinned
 CROSS JOIN LATERAL (SELECT (pinned.value->>'pin_jcs')::jsonb body) pin
 CROSS JOIN LATERAL jsonb_array_elements(pin.body->'arrays'||jsonb_build_array(jsonb_build_object('ref',pin.body->'binding'->'source','contract','WorkingImage'))) item
 WHERE parent.tenant_id=e.tenant_id AND parent.id=e.run_id
$patch$;
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_ASSEMBLY_RETENTION_DRIFT';END IF;
 EXECUTE replace(definition,needle,needle||extra);
 definition:=pg_get_functiondef('photo.v30_delivery_assembly_inventory(uuid,uuid,uuid,text)'::regprocedure);
 needle:='UNION ALL SELECT artifact_id,sha256,contract FROM photo.v2_task_inputs WHERE tenant_id=e.tenant_id AND task_id=e.parent_task_id';
 extra:=$patch$
 UNION ALL SELECT (item->'ref'->>'artifact_id')::uuid,item->'ref'->>'sha256',item->>'contract'
 FROM photo.runs parent CROSS JOIN LATERAL jsonb_each(parent.v30_watermark_sources) pinned
 CROSS JOIN LATERAL (SELECT (pinned.value->>'pin_jcs')::jsonb body) pin
 CROSS JOIN LATERAL jsonb_array_elements(pin.body->'arrays'||jsonb_build_array(jsonb_build_object('ref',pin.body->'binding'->'source','contract','WorkingImage'))) item
 WHERE parent.tenant_id=e.tenant_id AND parent.id=e.run_id
$patch$;
 -- Keep the parent input contract filter attached to its original SELECT.
 needle:=needle||E'\n AND contract NOT IN(''Params'',''InputClosureChunk'',''InputClosureManifest'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'WATERMARK_ASSEMBLY_INVENTORY_DRIFT';END IF;
 EXECUTE replace(definition,needle,needle||extra);
END $$;
-- Function OIDs, owners and role grants preserved; no new public ABI.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
