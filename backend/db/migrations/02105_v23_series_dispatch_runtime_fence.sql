-- CPU series.balance has no model_id, yet qualified Apply tasks carry an
-- exact runtime model_lock_id. Both resource admission and targeted worker
-- selection must honor it before claim_task performs its final check.
-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text; replacement text;
BEGIN
 definition:=pg_get_functiondef(
  'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure);
 needle:='AND (op.model_id IS NULL OR w.lock_id=model_lock)';
 replacement:='AND (op.model_id IS NOT NULL AND w.lock_id=model_lock OR
  op.model_id IS NULL AND (t.operation<>''series.balance'' OR
   t.execution_profile->>''model_lock_id'' IS NULL OR
   w.lock_id=t.execution_profile->>''model_lock_id''))';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected resource dispatch worker predicate';
 END IF;
 EXECUTE replace(definition,needle,replacement);
 definition:=pg_get_functiondef(
  'photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure);
 needle:='AND ((reserved.command->''model'')=''null''::jsonb OR w.lock_id=reserved.command->''model''->>''lock_id'')';
 replacement:=needle||'
  AND (t.operation<>''series.balance'' OR
   t.execution_profile->>''model_lock_id'' IS NULL OR
   w.lock_id=t.execution_profile->>''model_lock_id'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN
  RAISE EXCEPTION 'Unexpected targeted dispatch worker predicate';
 END IF;
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
