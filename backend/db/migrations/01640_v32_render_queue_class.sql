-- +goose Up
-- Render queue is frozen by trusted preview/batch compilers before hashing.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:='config:=p_config_jcs::jsonb;config_sha:=encode(sha256(convert_to(p_config_jcs,''UTF8'')),''hex'');';
BEGIN
 definition:=pg_get_functiondef('photo.begin_v26_render_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,text,bigint)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected render queue config definition'; END IF;
 EXECUTE replace(definition,needle,needle||'
 IF config ? ''queue_class'' AND NOT(config->''queue_class''=''"batch"''::jsonb OR config->''queue_class''=''"interactive"''::jsonb) THEN RETURN false; END IF;');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
