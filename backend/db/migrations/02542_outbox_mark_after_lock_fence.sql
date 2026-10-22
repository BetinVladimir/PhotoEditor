-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.mark_outbox_sent(p_kind text,p_id uuid,p_lease timestamptz)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE held_until timestamptz;already_sent timestamptz;
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN('task','mail') THEN RAISE EXCEPTION 'invalid outbox kind' USING ERRCODE='22023';END IF;
 IF p_lease IS NULL THEN RETURN false;END IF;
 IF p_kind='task' THEN
  SELECT publish_lease_until,sent_at INTO held_until,already_sent FROM photo.outbox WHERE id=p_id FOR UPDATE;
 ELSE
  SELECT publish_lease_until,sent_at INTO held_until,already_sent FROM photo.auth_mail_outbox WHERE id=p_id FOR UPDATE;
 END IF;
 IF NOT FOUND OR held_until IS DISTINCT FROM p_lease THEN RETURN false;END IF;
 -- 02542: read the clock only after row lock acquisition. Exact completed receipt may replay.
 IF already_sent IS NULL AND (held_until IS NULL OR held_until<=clock_timestamp()) THEN RETURN false;END IF;
 IF p_kind='task' THEN
  UPDATE photo.outbox SET sent_at=coalesce(sent_at,clock_timestamp()) WHERE id=p_id;
 ELSE
  UPDATE photo.auth_mail_outbox SET sent_at=coalesce(sent_at,clock_timestamp()) WHERE id=p_id;
 END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.mark_outbox_sent(text,uuid,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.mark_outbox_sent(text,uuid,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.mark_outbox_sent(text,uuid,timestamptz) TO photo_publisher;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
