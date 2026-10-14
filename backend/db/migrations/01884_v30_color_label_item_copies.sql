-- +goose Up
-- Preserve imported XMP color labels when an item is copied by Apply or bulk copy/move.
-- Both functions are patched from their installed definitions, so their existing
-- security attributes and all unrelated adoption/bulk rules stay intact.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text; replacement text;
BEGIN
 definition:=pg_get_functiondef('photo.adopt_apply_results(uuid,uuid,uuid)'::regprocedure);
 needle:='rating,flag,tags,created_by_run_id,source_item_id)';
 replacement:='rating,flag,color_label,tags,created_by_run_id,source_item_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected Apply item columns';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='i.rating,i.flag,i.tags,p_run,i.id';
 replacement:='i.rating,i.flag,i.color_label,i.tags,p_run,i.id';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected Apply item projection';END IF;
 EXECUTE replace(definition,needle,replacement);

 definition:=pg_get_functiondef('photo.process_bulk_chunk(uuid,uuid)'::regprocedure);
 needle:='rating,flag,tags,source_item_id)';
 replacement:='rating,flag,color_label,tags,source_item_id)';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected bulk item columns';END IF;
 definition:=replace(definition,needle,replacement);
 needle:='i.moderation_state,i.rating,i.flag,i.tags,i.id';
 replacement:='i.moderation_state,i.rating,i.flag,i.color_label,i.tags,i.id';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected bulk item projection';END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
