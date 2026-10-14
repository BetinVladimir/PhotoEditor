-- +goose Up
-- Admission must use the bucket of the specific verified source archive.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_runtime_ready(p_bucket text)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT p_bucket IS NOT NULL AND EXISTS(
  SELECT 1 FROM photo.v30_import_runtime_leases scheduler
  JOIN photo.v30_import_adopter_runtime_leases adopter
    ON scheduler.bucket=adopter.bucket
  WHERE scheduler.bucket=p_bucket
    AND scheduler.expires_at>clock_timestamp()
    AND adopter.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_runtime_ready(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_runtime_ready(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_runtime_ready(text) TO photo_api;

-- +goose StatementBegin
DO $$
DECLARE definition text; old_gate constant text :=
 'IF photo.v30_import_runtime_ready() IS DISTINCT FROM true THEN RETURN false;END IF;';
BEGIN
 SELECT pg_get_functiondef(
  'photo.begin_v30_interchange_parse_run(uuid,uuid,uuid,text,text,text,text,text)'::regprocedure)
 INTO definition;
 IF (length(definition)-length(replace(definition,old_gate,'')))/length(old_gate)<>1 THEN
  RAISE EXCEPTION 'Unexpected V30 import readiness gate';
 END IF;
 EXECUTE replace(definition,old_gate,
  'IF photo.v30_import_runtime_ready((SELECT a.bucket FROM photo.artifacts a '
  ||'WHERE a.tenant_id=p_tenant AND a.id=(bundle->>''artifact_id'')::uuid '
  ||'AND a.sha256=bundle->>''sha256'' AND a.state=''ready'')) '
  ||'IS DISTINCT FROM true THEN RETURN false;END IF;');
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
