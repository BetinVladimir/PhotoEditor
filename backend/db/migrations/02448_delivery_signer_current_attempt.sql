-- +goose Up
-- A retained signer receipt must still be the task's current canonical attempt.
-- Preserve all installed download authority/ACL checks while adding this fence.
-- +goose StatementBegin
DO $$
DECLARE definition text;
 anchor text := 'AND signer_result.id=pm.adopted_result_id AND signer_result.task_id=signer_task.id';
BEGIN
 SELECT pg_get_functiondef('photo.v30_delivery_download_target(uuid,uuid,uuid,uuid,text,integer,uuid,text)'::regprocedure) INTO definition;
 IF definition IS NULL OR
  (length(definition)-length(replace(definition,anchor,'')))/length(anchor)<>1 THEN
  RAISE EXCEPTION 'DELIVERY_SIGNER_CURRENT_ATTEMPT_SHAPE';
 END IF;
 definition:=replace(definition,anchor,anchor||E'\n    AND signer_result.attempt=signer_task.attempt\n    AND signer_result.fencing_token=signer_task.fencing_token');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
