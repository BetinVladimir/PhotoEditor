-- +goose Up
-- Metadata is produced for the original unsigned export before delegated
-- signing. Preserve every result/source guard while permitting a frozen
-- required policy through that pre-signing stage.
-- +goose StatementBegin
DO $$ DECLARE definition text;needle text;replacement text;signature text;
BEGIN
 needle:='frozen->''policy''->>''provenance'' IS DISTINCT FROM ''off''';
 replacement:='photo.v31_delivery_frozen_signing_choice_valid(frozen->''policy'') IS DISTINCT FROM true';
 FOREACH signature IN ARRAY ARRAY[
  'photo.begin_v30_metadata_barrier(uuid,uuid,integer,bigint,uuid,text,text,text,bigint)',
  'photo.adopt_v30_delivery_metadata(uuid,uuid,integer,bigint,uuid,text,text,text,text,text,bigint)'
 ] LOOP
  definition:=pg_get_functiondef(to_regprocedure(signature));
  IF definition IS NULL OR (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'DELIVERY_METADATA_SIGNING_CHOICE_SHAPE: %',signature;
  END IF;
  EXECUTE replace(definition,needle,replacement);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
