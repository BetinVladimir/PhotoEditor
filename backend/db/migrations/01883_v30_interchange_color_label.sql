-- +goose Up
-- The fixed XMP palette is stored only on newly imported collection items.
-- Historical manifests omit label, so their canonical bytes and receipts stay
-- unchanged. All three replaced function bodies are pinned to predecessor text.
ALTER TABLE photo.collection_items ADD COLUMN color_label text
 CHECK (color_label IN ('Red','Yellow','Green','Blue','Purple'));

-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.v30_interchange_import_document_scope_pre_evidence(uuid,uuid,text,uuid,text,text)'::regprocedure);
 needle:='(SELECT count(*) FROM jsonb_object_keys(entry))<>9';
 replacement:='(SELECT count(*) FROM jsonb_object_keys(entry)) NOT IN (9,10)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import plan entry shape';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='OR entry->''crop'' IS DISTINCT FROM original->''crop'' THEN RETURN NULL;END IF;';
 replacement:='OR entry->''crop'' IS DISTINCT FROM original->''crop''
    OR entry->''label'' IS DISTINCT FROM original->''label''
    OR (entry ? ''label'' AND entry->>''label'' NOT IN (''Red'',''Yellow'',''Green'',''Blue'',''Purple''))
    OR (entry ? ''label'' AND NOT (original ? ''label''))
   THEN RETURN NULL;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import plan entry binding';END IF;
 EXECUTE replace(definition,needle,replacement);

 definition:=pg_get_functiondef('photo.prepare_v30_interchange_draft_collection(uuid,uuid,text)'::regprocedure);
 needle:='rating integer;flag text;position bigint;';
 replacement:='rating integer;flag text;color_label text;position bigint;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import collection variables';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='position:=(row->>''ordinal'')::bigint*1024;';
 replacement:='color_label:=entry->>''label'';position:=(row->>''ordinal'')::bigint*1024;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import collection source';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='IF aid IS NULL OR flag IS NULL OR rating NOT BETWEEN 0 AND 5 THEN';
 replacement:='IF aid IS NULL OR flag IS NULL OR rating NOT BETWEEN 0 AND 5
   OR (color_label IS NOT NULL AND color_label NOT IN (''Red'',''Yellow'',''Green'',''Blue'',''Purple'')) THEN';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import collection validation';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='rating,flag,created_by_run_id)';replacement:='rating,flag,color_label,created_by_run_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import collection insert columns';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='rating,flag,w.run_id)';replacement:='rating,flag,color_label,w.run_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import collection insert values';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='OR i.flag IS DISTINCT FROM flag THEN';
 replacement:='OR i.flag IS DISTINCT FROM flag OR i.color_label IS DISTINCT FROM color_label THEN';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import collection replay guard';END IF;
 EXECUTE replace(definition,needle,replacement);

 definition:=pg_get_functiondef('photo.v30_interchange_metadata_receipts(uuid,uuid,text)'::regprocedure);
 needle:='''master'',row->''master'',''item_id'',i.id,''rating'',i.rating,''flag'',i.flag));';
 replacement:='''master'',row->''master'',''item_id'',i.id,''rating'',i.rating,''flag'',i.flag)
    || CASE WHEN i.color_label IS NULL THEN ''{}''::jsonb
      ELSE jsonb_build_object(''label'',i.color_label) END);';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected import metadata receipt projection';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
