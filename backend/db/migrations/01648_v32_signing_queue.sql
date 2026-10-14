-- +goose Up
-- Signing execution_pool remains signer; scheduling queue is frozen batch.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v31_signing_draft(uuid,uuid,uuid,text,text,text,text)'::regprocedure);
 needle:=' IF config IS DISTINCT FROM expected_config THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected signing queue definition'; END IF;
 EXECUTE replace(definition,needle,$patch$
 IF saved ? 'queue_class' THEN
  IF saved->'queue_class' IS DISTINCT FROM '"batch"'::jsonb THEN RETURN false;END IF;
  expected_config:=expected_config||jsonb_build_object('queue_class','batch');
 END IF;
 IF config IS DISTINCT FROM expected_config THEN RETURN false;END IF;
$patch$);
 definition:=pg_get_functiondef('photo.pin_v31_signing_pending(uuid,uuid,uuid,text,text,text)'::regprocedure);
 needle:=' IF jsonb_typeof(saved) IS DISTINCT FROM ''object'' OR';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected signing pending queue definition'; END IF;
 EXECUTE replace(definition,needle,' IF (saved ? ''queue_class'' AND saved->''queue_class'' IS DISTINCT FROM ''"batch"''::jsonb) OR jsonb_typeof(saved) IS DISTINCT FROM ''object'' OR');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
