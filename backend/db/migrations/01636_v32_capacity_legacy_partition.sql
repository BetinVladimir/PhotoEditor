-- +goose Up
-- V32-enabled service calls this picker for legacy runs only. Original picker
-- remains available for deployments that have not enabled V32 policy.
-- Pause old scheduler binaries before switching the deployment to V32.
-- +goose StatementBegin
DO $$
DECLARE definition text; needle text:='AND r.status IN (''queued'',''running'')';
BEGIN
 definition:=pg_get_functiondef('photo.pick_ready_task()'::regprocedure);
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>2 THEN
  RAISE EXCEPTION 'Unexpected legacy partition picker definition'; END IF;
 IF (length(definition)-length(replace(definition,'photo.pick_ready_task()','')))/length('photo.pick_ready_task()')<>1 THEN
  RAISE EXCEPTION 'Unexpected legacy picker signature'; END IF;
 definition:=replace(definition,'photo.pick_ready_task()','photo.pick_ready_legacy_task()');
 EXECUTE replace(definition,needle,'AND r.kind<>''v2'' '||needle);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pick_ready_legacy_task() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pick_ready_legacy_task() FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.pick_ready_legacy_task() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
