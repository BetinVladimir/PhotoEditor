-- +goose Up
-- Preserve the queue choice saved in pending, including historical omission.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text;
BEGIN
 definition:=pg_get_functiondef('photo.begin_v30_delivery_create_run(uuid,uuid,uuid,text,text,text)'::regprocedure);
 needle:=' config:=p_config::jsonb;IF config IS DISTINCT FROM expected THEN RETURN false;END IF;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected delivery pending queue definition'; END IF;
 EXECUTE replace(definition,needle,$patch$
 IF saved ? 'queue_class' THEN
  IF saved->'queue_class' IS DISTINCT FROM '"batch"'::jsonb THEN RETURN false;END IF;
  expected:=expected||jsonb_build_object('queue_class','batch');
 END IF;
 config:=p_config::jsonb;IF config IS DISTINCT FROM expected THEN RETURN false;END IF;
$patch$);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
