-- +goose Up
-- Complete statement snapshot; a prefix would starve later tenant UUIDs.
-- Preserve function identity, ACL and all existing eligibility predicates.
-- +goose StatementBegin
DO $$
DECLARE definition text;needle text:='ORDER BY t.tenant_id,t.created_at,t.id LIMIT 10001;';
BEGIN
 definition:=pg_get_functiondef('photo.capacity_queue_candidates(text)'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected capacity candidate snapshot definition';
 END IF;
 EXECUTE replace(definition,needle,'ORDER BY t.tenant_id,t.created_at,t.id;');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
