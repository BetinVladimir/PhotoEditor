-- +goose Up
-- Negative primary is V2; preview/quality child runs remain kind=preview.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text;
BEGIN
 definition:=pg_get_functiondef('photo.save_v29_negative_draft(uuid,uuid,uuid,text)'::regprocedure);
 needle:=' SELECT * INTO old FROM photo.runs WHERE id=v_run FOR UPDATE;';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'Unexpected negative primary queue producer';END IF;
 EXECUTE replace(definition,needle,needle||$patch$
 config:=photo.v32_frozen_preview_config(config,old.config_snapshot);
 IF config IS NULL THEN RETURN false;END IF;
$patch$);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
