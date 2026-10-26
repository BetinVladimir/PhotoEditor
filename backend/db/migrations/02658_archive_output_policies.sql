-- +goose Up
-- archive.build streams at most10GiB payload /5000 entries. Its validated ZIP
-- reservation adds1MiB manifest allowance plus4096 bytes per entry. This is an
-- output authorization bound, not physical max-volume qualification evidence.
-- No changes to roles, functions, existing policies, task state or worker admission.
-- +goose StatementBegin
DO $guard$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 2657 THEN
  RAISE EXCEPTION 'ARCHIVE_OUTPUT_POLICY_HEAD_PREIMAGE';
 END IF;
 LOCK TABLE photo.operation_output_policies IN SHARE ROW EXCLUSIVE MODE;
 PERFORM 1 FROM photo.operation_registry
 WHERE operation='archive.build' AND version='1' AND resource_class='archive'
  AND routing_key='io.archive' AND model_id IS NULL AND timeout_seconds=7200
  AND backends='["cpu"]'::jsonb FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'ARCHIVE_OUTPUT_POLICY_REGISTRY_PREIMAGE';END IF;
 IF EXISTS(SELECT FROM photo.operation_output_policies WHERE operation='archive.build') THEN
  RAISE EXCEPTION 'ARCHIVE_OUTPUT_POLICY_ROWS_PREIMAGE';
 END IF;
 INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
  ('archive.build','1','archive','["application/zip"]'::jsonb,10758946816,1),
  ('archive.build','1','archive_manifest','["application/json"]'::jsonb,1048576,1);
END $guard$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
