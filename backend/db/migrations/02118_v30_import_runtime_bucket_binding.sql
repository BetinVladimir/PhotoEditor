-- +goose Up
-- Old, unbound leases cannot admit new imports after this migration.
ALTER TABLE photo.v30_import_runtime_leases ADD COLUMN bucket text
 CHECK (bucket IS NULL OR length(bucket) BETWEEN 1 AND 255);
ALTER TABLE photo.v30_import_adopter_runtime_leases ADD COLUMN bucket text
 CHECK (bucket IS NULL OR length(bucket) BETWEEN 1 AND 255);

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_import_runtime_bound(p_instance uuid,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
    OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 1 AND 255 THEN
  RETURN false;
 END IF;
 UPDATE photo.v30_import_runtime_leases
 SET expires_at=clock_timestamp()+interval '30 seconds',bucket=p_bucket
 WHERE instance_id=p_instance;
 IF FOUND THEN RETURN true;END IF;
 UPDATE photo.v30_import_runtime_leases
 SET instance_id=p_instance,expires_at=clock_timestamp()+interval '30 seconds',bucket=p_bucket
 WHERE instance_id=(SELECT old.instance_id FROM photo.v30_import_runtime_leases old
  WHERE old.expires_at<clock_timestamp()-interval '1 day'
  ORDER BY old.expires_at LIMIT 1 FOR UPDATE SKIP LOCKED);
 IF FOUND THEN RETURN true;END IF;
 INSERT INTO photo.v30_import_runtime_leases(instance_id,expires_at,bucket)
 VALUES(p_instance,clock_timestamp()+interval '30 seconds',p_bucket)
 ON CONFLICT(instance_id) DO UPDATE SET expires_at=excluded.expires_at,bucket=excluded.bucket;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_import_runtime_bound(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_import_runtime_bound(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_import_runtime_bound(uuid,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.announce_v30_import_adopter_runtime_bound(p_instance uuid,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_instance IS NULL OR p_instance='00000000-0000-0000-0000-000000000000'::uuid
    OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 1 AND 255 THEN
  RETURN false;
 END IF;
 UPDATE photo.v30_import_adopter_runtime_leases
 SET expires_at=clock_timestamp()+interval '30 seconds',bucket=p_bucket
 WHERE instance_id=p_instance;
 IF FOUND THEN RETURN true;END IF;
 UPDATE photo.v30_import_adopter_runtime_leases
 SET instance_id=p_instance,expires_at=clock_timestamp()+interval '30 seconds',bucket=p_bucket
 WHERE instance_id=(SELECT old.instance_id FROM photo.v30_import_adopter_runtime_leases old
  WHERE old.expires_at<clock_timestamp()-interval '1 day'
  ORDER BY old.expires_at LIMIT 1 FOR UPDATE SKIP LOCKED);
 IF FOUND THEN RETURN true;END IF;
 INSERT INTO photo.v30_import_adopter_runtime_leases(instance_id,expires_at,bucket)
 VALUES(p_instance,clock_timestamp()+interval '30 seconds',p_bucket)
 ON CONFLICT(instance_id) DO UPDATE SET expires_at=excluded.expires_at,bucket=excluded.bucket;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.announce_v30_import_adopter_runtime_bound(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.announce_v30_import_adopter_runtime_bound(uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.announce_v30_import_adopter_runtime_bound(uuid,text) TO photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.announce_v30_import_runtime(uuid) FROM photo_scheduler;
REVOKE EXECUTE ON FUNCTION photo.announce_v30_import_adopter_runtime(uuid) FROM photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v30_import_runtime_ready()
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v30_import_runtime_leases scheduler
  JOIN photo.v30_import_adopter_runtime_leases adopter
    ON scheduler.bucket=adopter.bucket
  WHERE scheduler.bucket IS NOT NULL
    AND scheduler.expires_at>clock_timestamp()
    AND adopter.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_runtime_ready() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_runtime_ready() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_runtime_ready() TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
