-- +goose Up
-- Normalize previously authorized inline deletes into the explicit allowlist.
-- This grants no new DELETE rights and changes no retained tenant tombstones.
-- +goose StatementBegin
DO $$
DECLARE definition text; name text; needle text;
 entries constant text[]:=ARRAY['export_delivery_tickets','export_delivery_batches',
  'editor_effect_builds','editor_recipe_bridges','apply_culling_reviews'];
 anchor constant text:='tables_to_purge constant text[]:=ARRAY[';
 prefix text:='';
BEGIN
 definition:=pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure);
 IF definition IS NULL OR
  (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN
  RAISE EXCEPTION 'TENANT_PURGE_ALLOWLIST_NORMALIZATION_SHAPE';
 END IF;
 FOREACH name IN ARRAY entries LOOP
  needle:=format(' DELETE FROM photo.%I WHERE tenant_id=j.tenant_id;',name);
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1
   OR NOT has_table_privilege('photo_fn_owner',format('photo.%I',name),'DELETE') THEN
   RAISE EXCEPTION 'TENANT_PURGE_ALLOWLIST_ENTRY_SHAPE: %',name;
  END IF;
  definition:=replace(definition,needle,'');
  prefix:=prefix||quote_literal(name)||',';
 END LOOP;
 EXECUTE replace(definition,anchor,anchor||prefix);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
