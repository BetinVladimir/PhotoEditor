-- +goose Up
-- Narrow diagnostic for publisher recovery; no payload/table access grants.
-- +goose StatementBegin
CREATE FUNCTION photo.restore_task_outbox_pending(p_database text) RETURNS bigint
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_database IS NULL OR p_database<>current_database() THEN
  RAISE EXCEPTION 'RESTORE_REPLAY_DATABASE_CHANGED' USING ERRCODE='22023';
 END IF;
 RETURN (SELECT count(*) FROM photo.outbox WHERE sent_at IS NULL);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.restore_task_outbox_pending(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.restore_task_outbox_pending(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.restore_task_outbox_pending(text) TO photo_publisher;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
