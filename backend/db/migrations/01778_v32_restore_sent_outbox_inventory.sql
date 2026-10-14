-- +goose Up
-- Publisher-only, read-only keyset of task envelopes marked sent since the
-- database clock captured immediately before replay dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.restore_task_outbox_sent_page(p_database text,p_since timestamptz,p_after uuid,p_limit integer)
RETURNS TABLE(id uuid,tenant_id uuid,event_id uuid,destination text,routing_key text,payload jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v record;
BEGIN
 IF p_database IS NULL OR p_database<>current_database() OR p_since IS NULL
  OR p_since>clock_timestamp() OR p_since<clock_timestamp()-interval '20 minutes'
  OR p_limit IS NULL OR p_limit<1 OR p_limit>128 THEN
  RAISE EXCEPTION 'RESTORE_SENT_OUTBOX_SCOPE' USING ERRCODE='22023';
 END IF;
 FOR v IN SELECT o.id,o.tenant_id,o.event_id,o.destination,o.routing_key,o.payload
 FROM photo.outbox o WHERE o.sent_at>=p_since AND (p_after IS NULL OR o.id>p_after)
 ORDER BY o.id LIMIT p_limit LOOP
  IF v.id='00000000-0000-0000-0000-000000000000'::uuid
   OR v.tenant_id='00000000-0000-0000-0000-000000000000'::uuid
   OR v.event_id='00000000-0000-0000-0000-000000000000'::uuid
   OR v.destination='' OR octet_length(v.destination)>4096 OR octet_length(v.routing_key)>4096
   OR jsonb_typeof(v.payload)<>'object' OR octet_length(v.payload::text)>1048576 THEN
   RAISE EXCEPTION 'RESTORE_SENT_OUTBOX_ENVELOPE' USING ERRCODE='22023';
  END IF;
  id:=v.id;tenant_id:=v.tenant_id;event_id:=v.event_id;
  destination:=v.destination;routing_key:=v.routing_key;payload:=v.payload;
  RETURN NEXT;
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.restore_task_outbox_sent_page(text,timestamptz,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.restore_task_outbox_sent_page(text,timestamptz,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.restore_task_outbox_sent_page(text,timestamptz,uuid,integer) TO photo_publisher;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
