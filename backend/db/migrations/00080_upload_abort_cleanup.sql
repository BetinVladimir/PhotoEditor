-- +goose Up
-- Durable cleanup is required because aborting object-storage multipart state
-- cannot participate in the database transaction which releases quota.
CREATE TABLE photo.upload_abort_cleanup (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 upload_id uuid NOT NULL UNIQUE,
 tenant_id uuid NOT NULL,
 object_key text NOT NULL,
 multipart_id text NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','cleaning','cleaned')),
 next_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 token uuid,
 lease_until timestamptz,
 completed_at timestamptz,
 FOREIGN KEY(tenant_id,upload_id) REFERENCES photo.upload_sessions(tenant_id,id),
 CHECK(length(object_key) BETWEEN 1 AND 1024),
 CHECK(length(multipart_id) BETWEEN 1 AND 4096),
 CHECK((token IS NULL)=(lease_until IS NULL)),
 CHECK((state='cleaned')=(completed_at IS NOT NULL))
);
ALTER TABLE photo.upload_abort_cleanup ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.upload_abort_cleanup FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.upload_abort_cleanup TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.upload_abort_cleanup TO photo_fn_owner;
CREATE INDEX upload_abort_cleanup_due ON photo.upload_abort_cleanup(next_at,id) WHERE state<>'cleaned';

-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_upload_abort_cleanup() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.state IN ('aborted','expired') AND OLD.state IS DISTINCT FROM NEW.state AND NEW.multipart_id IS NOT NULL THEN
  INSERT INTO photo.upload_abort_cleanup(upload_id,tenant_id,object_key,multipart_id,next_at)
  VALUES(NEW.id,NEW.tenant_id,NEW.object_key,NEW.multipart_id,clock_timestamp())
  ON CONFLICT(upload_id) DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_upload_abort_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_upload_abort_cleanup() FROM PUBLIC;
CREATE TRIGGER enqueue_upload_abort_cleanup AFTER UPDATE OF state ON photo.upload_sessions
 FOR EACH ROW EXECUTE FUNCTION photo.enqueue_upload_abort_cleanup();

-- Expire one pre-completion upload per call. The usage row is always the first
-- aggregate lock; the reservation-state trigger in migration 79 settles files.
-- +goose StatementBegin
CREATE FUNCTION photo.expire_upload_session() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.upload_sessions%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'UPLOAD_EXPIRY_REQUIRES_READ_COMMITTED';END IF;
 SELECT x.* INTO hint FROM photo.upload_sessions x
 WHERE x.state IN ('initializing','uploading') AND x.expires_at<=clock_timestamp()
 ORDER BY x.expires_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 SELECT x.* INTO u FROM photo.upload_sessions x WHERE x.tenant_id=hint.tenant_id AND x.id=hint.id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR u.state NOT IN ('initializing','uploading') OR u.expires_at>clock_timestamp() THEN RETURN false;END IF;
 SELECT x.* INTO q FROM photo.quota_reservations x WHERE x.tenant_id=u.tenant_id AND x.id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.state<>'reserved' OR q.bytes<>u.expected_size OR q.files<>1 THEN
  RAISE EXCEPTION 'UPLOAD_EXPIRY_RESERVATION' USING ERRCODE='23514';
 END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes
 WHERE tenant_id=u.tenant_id AND reserved_bytes>=q.bytes AND reserved_files>=q.files;
 IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_EXPIRY_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=u.tenant_id AND id=q.id;
 UPDATE photo.upload_sessions SET state='expired',revision=revision+1 WHERE tenant_id=u.tenant_id AND id=u.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.expire_upload_session() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_upload_session() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expire_upload_session() TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_upload_abort_cleanup()
RETURNS TABLE(cleanup_id uuid,cleanup_token uuid,upload_id uuid,tenant_id uuid,object_key text,multipart_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.upload_abort_cleanup%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'UPLOAD_ABORT_REQUIRES_READ_COMMITTED';END IF;
 SELECT x.* INTO d FROM photo.upload_abort_cleanup x
 WHERE x.state<>'cleaned' AND x.next_at<=clock_timestamp() AND (x.lease_until IS NULL OR x.lease_until<=clock_timestamp())
 ORDER BY x.next_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.upload_abort_cleanup SET state='cleaning',token=fence,lease_until=clock_timestamp()+interval '60 seconds',
  next_at=clock_timestamp()+interval '60 seconds' WHERE id=d.id;
 RETURN QUERY SELECT d.id,fence,d.upload_id,d.tenant_id,d.object_key,d.multipart_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_upload_abort_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_upload_abort_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_upload_abort_cleanup() TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_upload_abort_cleanup(p_id uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.upload_abort_cleanup%ROWTYPE;
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.upload_abort_cleanup WHERE id=p_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF d.state='cleaned' THEN RETURN d.token=p_token;END IF;
 IF d.state<>'cleaning' OR d.token IS DISTINCT FROM p_token OR d.lease_until IS NULL OR d.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.upload_sessions u WHERE u.tenant_id=d.tenant_id AND u.id=d.upload_id
  AND u.state IN ('aborted','expired') AND u.object_key=d.object_key AND u.multipart_id=d.multipart_id) THEN RETURN false;END IF;
 -- Preserve the final token/expired lease so the exact finish replay is true.
 UPDATE photo.upload_abort_cleanup SET state='cleaned',completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_upload_abort_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_upload_abort_cleanup(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_upload_abort_cleanup(uuid,uuid) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
