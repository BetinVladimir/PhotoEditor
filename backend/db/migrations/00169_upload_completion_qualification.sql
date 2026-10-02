-- +goose Up
-- Qualify the usage lookup because tenant_id is also an OUT parameter of this
-- table-returning function. PostgreSQL otherwise raises SQLSTATE 42702 before
-- a completion lease can be issued.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_upload_completion()
RETURNS TABLE(upload_id uuid,completion_token uuid,tenant_id uuid,object_key text,multipart_id text,
 media_type text,filename text,assume_profile text,expected_bytes bigint,expected_sha text,artifact_id uuid,snapshot_id uuid,run_id uuid,parts jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.upload_sessions%ROWTYPE;u photo.upload_sessions%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO hint FROM photo.upload_sessions x WHERE x.state='completing' AND x.next_completion_at<=clock_timestamp()
 AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp()) ORDER BY x.next_completion_at,x.created_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM usage.id FROM photo.tenant_usage usage WHERE usage.tenant_id=hint.tenant_id FOR UPDATE;
 SELECT x.* INTO u FROM photo.upload_sessions x WHERE x.id=hint.id AND x.tenant_id=hint.tenant_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR u.multipart_id IS NULL OR u.media_type IS NULL OR u.staging_candidate_id IS NULL OR u.ingest_snapshot_id IS NULL OR u.ingest_run_id IS NULL
 OR u.actual_size IS NOT NULL OR u.staging_artifact_id IS NOT NULL THEN RETURN;END IF;
 IF (SELECT count(*) FROM photo.upload_parts p WHERE p.tenant_id=u.tenant_id AND p.upload_session_id=u.id AND p.state='completed')<1 THEN RETURN;END IF;
 fence:=gen_random_uuid();
 UPDATE photo.upload_sessions x SET completion_token=fence,completion_lease_until=clock_timestamp()+interval '60 seconds',next_completion_at=clock_timestamp()+interval '60 seconds' WHERE x.id=u.id;
 RETURN QUERY SELECT u.id,fence,u.tenant_id,u.object_key,u.multipart_id,u.media_type,u.filename,u.assume_profile,u.expected_size,u.expected_sha256,
  u.staging_candidate_id,u.ingest_snapshot_id,u.ingest_run_id,(SELECT jsonb_agg(jsonb_build_object('part_no',p.part_no,'etag',p.etag) ORDER BY p.part_no) FROM photo.upload_parts p WHERE p.tenant_id=u.tenant_id AND p.upload_session_id=u.id AND p.state='completed');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_upload_completion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_upload_completion() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_upload_completion() TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
