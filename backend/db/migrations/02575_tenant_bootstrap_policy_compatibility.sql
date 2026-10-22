-- +goose Up
-- Personal registration retains its historical 10 GiB / 10000 allowance.
-- Historical keys remain metadata, not aliases for modern policy fields.
-- In particular active_gpu_tasks never governed admission: installed readers
-- coalesce missing max_active_gpu_tasks to 1. Preserve that effective limit.
-- +goose StatementBegin
DO $migration$
DECLARE original text; updated text;
BEGIN
 SELECT pg_get_functiondef('photo.auth_bootstrap_user(text,text,boolean)'::regprocedure) INTO original;
 IF NOT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner
   WHERE p.oid='photo.auth_bootstrap_user(text,text,boolean)'::regprocedure
     AND p.prosecdef AND r.rolname='photo_fn_owner'
     AND p.proconfig=ARRAY['search_path=pg_catalog, photo'])
 OR (length(original)-length(replace(original,'''{"storage_bytes":10737418240,"source_files":10000,"active_runs":4,"active_gpu_tasks":1}''','')))
      /length('''{"storage_bytes":10737418240,"source_files":10000,"active_runs":4,"active_gpu_tasks":1}''')<>1
 OR (length(original)-length(replace(original,'''{"deleted_days":30,"artifact_days":30}''','')))
      /length('''{"deleted_days":30,"artifact_days":30}''')<>1 THEN
  RAISE EXCEPTION 'Unexpected auth bootstrap policy definition';
 END IF;
 updated:=replace(original,
  '''{"storage_bytes":10737418240,"source_files":10000,"active_runs":4,"active_gpu_tasks":1}''',
  '''{"storage_bytes":10737418240,"source_files":10000,"max_active_gpu_tasks":1}''');
 updated:=replace(updated,'''{"deleted_days":30,"artifact_days":30}''',
  '''{"processing_days":7,"exports_days":30,"temporary_hours":24}''');
 -- CREATE OR REPLACE retains the existing owner, OID and explicit ACL.
 EXECUTE updated;
END $migration$;
-- +goose StatementEnd

-- Left-side defaults only fill absent fields; existing modern and historical
-- custom values, including unknown stored metadata, survive verbatim.
UPDATE photo.tenants
SET quotas=CASE WHEN jsonb_typeof(quotas)='object'
                    AND (quotas ? 'active_runs' OR quotas ? 'active_gpu_tasks')
               THEN '{"max_active_gpu_tasks":1}'::jsonb || quotas ELSE quotas END,
    retention=CASE WHEN jsonb_typeof(retention)='object'
                       AND (retention ? 'deleted_days' OR retention ? 'artifact_days')
                  THEN '{"processing_days":7,"exports_days":30,"temporary_hours":24}'::jsonb || retention ELSE retention END,
    revision=revision+1
WHERE (jsonb_typeof(quotas)='object' AND (quotas ? 'active_runs' OR quotas ? 'active_gpu_tasks')
       AND NOT (quotas ? 'max_active_gpu_tasks'))
   OR (jsonb_typeof(retention)='object' AND (retention ? 'deleted_days' OR retention ? 'artifact_days')
       AND NOT (retention ?& ARRAY['processing_days','exports_days','temporary_hours']));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
