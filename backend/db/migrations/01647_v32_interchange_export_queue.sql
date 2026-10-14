-- +goose Up
-- Queue choice belongs to frozen export evidence and survives exact replay.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_interchange_export_run(uuid,uuid,uuid,text,text,text)'::regprocedure);
 needle:=' config:=p_config::jsonb;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected interchange export queue definition'; END IF;
 EXECUTE replace(definition,needle,$patch$
 IF frozen ? 'queue_class' THEN
  IF frozen->'queue_class' IS DISTINCT FROM '"batch"'::jsonb THEN RETURN false;END IF;
  expected:=expected||jsonb_build_object('queue_class','batch');
 END IF;
 config:=p_config::jsonb;
$patch$);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
