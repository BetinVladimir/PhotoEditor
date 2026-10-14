-- +goose Up
-- Session analysis compiler binds batch class before config hashing. Historical
-- absent-field snapshots and recovery receipts are never rewritten.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:='OR jsonb_typeof(p_config) IS DISTINCT FROM ''object''';
BEGIN
 definition:=pg_get_functiondef('photo.begin_v22_analysis_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,jsonb,uuid,uuid,text,text,jsonb,bigint,text,text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected session analysis queue config definition'; END IF;
 EXECUTE replace(definition,needle,needle||'
 OR (p_config ? ''queue_class'' AND p_config->''queue_class'' IS DISTINCT FROM ''"batch"''::jsonb)');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
