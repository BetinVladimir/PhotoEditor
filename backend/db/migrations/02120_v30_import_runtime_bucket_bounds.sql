-- +goose Up
-- Existing leases may be stale; new writes and readiness use S3 bucket bounds.
ALTER TABLE photo.v30_import_runtime_leases
 ADD CONSTRAINT v30_import_runtime_bucket_bounds
 CHECK(bucket IS NULL OR length(bucket) BETWEEN 3 AND 63) NOT VALID;
ALTER TABLE photo.v30_import_adopter_runtime_leases
 ADD CONSTRAINT v30_import_adopter_runtime_bucket_bounds
 CHECK(bucket IS NULL OR length(bucket) BETWEEN 3 AND 63) NOT VALID;

-- +goose StatementBegin
DO $$
DECLARE definition text; signature regprocedure;
 old_guard constant text := 'length(p_bucket) NOT BETWEEN 1 AND 255';
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.announce_v30_import_runtime_bound(uuid,text)'::regprocedure,
  'photo.announce_v30_import_adopter_runtime_bound(uuid,text)'::regprocedure
 ] LOOP
  definition:=pg_get_functiondef(signature);
  IF (length(definition)-length(replace(definition,old_guard,'')))/length(old_guard)<>1 THEN
   RAISE EXCEPTION 'Unexpected V30 import bucket announce guard: %',signature;
  END IF;
  EXECUTE replace(definition,old_guard,'length(p_bucket) NOT BETWEEN 3 AND 63');
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE definition text;signature regprocedure;old_guard text;new_guard text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.v30_import_runtime_ready()'::regprocedure,
  'photo.v30_import_runtime_ready(text)'::regprocedure
 ] LOOP
  definition:=pg_get_functiondef(signature);
  IF signature='photo.v30_import_runtime_ready()'::regprocedure THEN
   old_guard:='scheduler.bucket IS NOT NULL';
   new_guard:='length(scheduler.bucket) BETWEEN 3 AND 63';
  ELSE
   old_guard:='p_bucket IS NOT NULL AND EXISTS';
   new_guard:='length(p_bucket) BETWEEN 3 AND 63 AND EXISTS';
  END IF;
  IF (length(definition)-length(replace(definition,old_guard,'')))/length(old_guard)<>1 THEN
   RAISE EXCEPTION 'Unexpected V30 import bucket readiness guard: %',signature;
  END IF;
  EXECUTE replace(definition,old_guard,new_guard);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
