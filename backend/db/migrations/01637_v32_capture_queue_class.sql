-- +goose Up
-- New capture compilers bind queue_class before hashing. Historical config/hash
-- pairs remain unchanged and readable; no UPDATE of immutable run snapshots.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:='policy_sha:=config->''v24_capture''->>''policy_sha'';';
BEGIN
 definition:=pg_get_functiondef('photo.begin_capture_draft(uuid,uuid,uuid,uuid,uuid,text,bigint)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected capture queue config definition'; END IF;
 EXECUTE replace(definition,needle,'IF config ? ''queue_class'' AND config->''queue_class'' IS DISTINCT FROM ''"capture"''::jsonb THEN RETURN false; END IF;
 '||needle);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
