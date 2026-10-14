-- +goose Up
-- Queue class is hashed by the compiler; historical snapshots are untouched.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:='config:=p_config_jcs::jsonb;config_sha:=encode(sha256(convert_to(p_config_jcs,''UTF8'')),''hex'');';
BEGIN
 definition:=pg_get_functiondef('photo.begin_v25_lens_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected lens queue config definition'; END IF;
 EXECUTE replace(definition,needle,needle||'
 IF config ? ''queue_class'' AND config->''queue_class'' IS DISTINCT FROM ''"batch"''::jsonb THEN RETURN false; END IF;');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
