-- +goose Up
-- Current retry authority may differ from immutable extraction session provenance.
-- +goose StatementBegin
DO $$
DECLARE definition text;target text;needle text;
BEGIN
 FOREACH target IN ARRAY ARRAY[
 'photo.v30_metadata_source_scope(uuid,uuid,uuid,uuid,text,text,text,text[])',
 'photo.v30_metadata_parent_inventory_ready(uuid,uuid,uuid,uuid,jsonb,text)'] LOOP
  definition:=pg_get_functiondef(target::regprocedure);
  needle:='photo.v30_metadata_source_authorized(';
  IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
   RAISE EXCEPTION 'METADATA_RETRY_AUTHORITY_DEFINITION_DRIFT: %',target;
  END IF;
  EXECUTE replace(definition,needle,'photo.v30_metadata_source_reauthorized(');
 END LOOP;
 definition:=pg_get_functiondef('photo.v30_metadata_parent_inventory_ready_before_live_sources(uuid,uuid,uuid,uuid,jsonb,text)'::regprocedure);
 needle:='envelope->''scope''->>''session_id'' IS DISTINCT FROM p_session::text';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'METADATA_RETRY_PROVENANCE_DEFINITION_DRIFT';
 END IF;
 -- Outer parent inventory validator always repeats current live authorization.
 EXECUTE replace(definition,needle,'nullif(envelope->''scope''->>''session_id'','''')::uuid IS NULL');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
