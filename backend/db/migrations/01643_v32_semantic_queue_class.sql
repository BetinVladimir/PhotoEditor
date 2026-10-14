-- +goose Up
-- Preserve the current public wrapper and all delegated qualification checks.
-- Only the transfer-plan config gains a compiler-selected batch queue.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:=E'BEGIN\n';
BEGIN
 definition:=pg_get_functiondef('photo.create_v2_draft_shell(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected semantic queue shell wrapper'; END IF;
 EXECUTE replace(definition,needle,needle||'
 IF p_workflow=''semantic'' AND p_config ? ''v27_semantic'' AND p_config ? ''queue_class''
 AND p_config->''queue_class'' IS DISTINCT FROM ''"batch"''::jsonb THEN RETURN false; END IF;
');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
