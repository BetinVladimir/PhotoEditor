-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.claim_outbox_batch(p_kind text,p_limit int,p_lease_seconds int)
RETURNS TABLE(id uuid,event_id uuid,destination text,routing_key text,payload jsonb,lease timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE until_time timestamptz:=clock_timestamp()+make_interval(secs=>p_lease_seconds);
BEGIN
 IF p_limit NOT BETWEEN 1 AND 100 OR p_lease_seconds NOT BETWEEN 10 AND 120 THEN RAISE EXCEPTION 'invalid outbox claim' USING ERRCODE='22023'; END IF;
 IF p_kind='task' THEN
  RETURN QUERY WITH picked AS (
   SELECT o.id FROM photo.outbox o WHERE o.sent_at IS NULL AND (o.publish_lease_until IS NULL OR o.publish_lease_until<clock_timestamp())
   AND o.destination IN ('photo.commands.v1','photo.events.v1') ORDER BY o.created_at,o.id LIMIT p_limit FOR UPDATE SKIP LOCKED
  ) UPDATE photo.outbox o SET publish_lease_until=until_time,attempt=o.attempt+1 FROM picked WHERE o.id=picked.id
  RETURNING o.id,o.event_id,o.destination,o.routing_key,o.payload,o.publish_lease_until;
 ELSIF p_kind='mail' THEN
  RETURN QUERY WITH picked AS (
   SELECT o.id FROM photo.auth_mail_outbox o JOIN photo.mail_payloads m ON m.id=o.payload_id
   WHERE o.sent_at IS NULL AND (o.publish_lease_until IS NULL OR o.publish_lease_until<clock_timestamp())
   AND m.expires_at>clock_timestamp() AND m.status IN ('queued','retry') ORDER BY o.created_at,o.id LIMIT p_limit FOR UPDATE OF o SKIP LOCKED
  ) UPDATE photo.auth_mail_outbox o SET publish_lease_until=until_time FROM picked,photo.mail_payloads m WHERE o.id=picked.id AND m.id=o.payload_id
  RETURNING o.id,o.event_id,'photo.commands.v1'::text,'mail.send'::text,
  jsonb_build_object('schema_version',1,'event_id',o.event_id,'operation','mail.send','payload_id',m.id,'attempt',m.attempt+1,'correlation_id',o.event_id),o.publish_lease_until;
 ELSE RAISE EXCEPTION 'invalid outbox kind' USING ERRCODE='22023'; END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_outbox_batch(text,int,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_outbox_batch(text,int,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_outbox_batch(text,int,int) TO photo_publisher;
-- +goose StatementBegin
CREATE FUNCTION photo.mark_outbox_sent(p_kind text,p_id uuid,p_lease timestamptz)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE affected int;
BEGIN
 IF p_kind='task' THEN
  UPDATE photo.outbox SET sent_at=COALESCE(sent_at,clock_timestamp()) WHERE id=p_id AND publish_lease_until=p_lease AND (sent_at IS NOT NULL OR publish_lease_until>clock_timestamp());
 ELSIF p_kind='mail' THEN
  UPDATE photo.auth_mail_outbox SET sent_at=COALESCE(sent_at,clock_timestamp()) WHERE id=p_id AND publish_lease_until=p_lease AND (sent_at IS NOT NULL OR publish_lease_until>clock_timestamp());
 ELSE RAISE EXCEPTION 'invalid outbox kind' USING ERRCODE='22023'; END IF;
 GET DIAGNOSTICS affected=ROW_COUNT; RETURN affected=1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.mark_outbox_sent(text,uuid,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.mark_outbox_sent(text,uuid,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.mark_outbox_sent(text,uuid,timestamptz) TO photo_publisher;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
