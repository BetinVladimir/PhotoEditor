-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='photo_mail') THEN CREATE ROLE photo_mail NOLOGIN NOBYPASSRLS; END IF; END $$;
-- +goose StatementEnd
GRANT USAGE ON SCHEMA photo TO photo_mail;
ALTER TABLE photo.mail_payloads ADD CONSTRAINT mail_state CHECK(status IN ('queued','sending','retry','sent','failed','expired'));
-- +goose StatementBegin
CREATE FUNCTION photo.claim_mail(p_payload uuid,p_attempt int,p_worker uuid)
RETURNS TABLE(outcome text,fence bigint,recipient_cipher bytea,payload_cipher bytea,key_id text,message_id text,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m photo.mail_payloads%ROWTYPE;
BEGIN
 SELECT * INTO m FROM photo.mail_payloads WHERE id=p_payload FOR UPDATE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::bigint,NULL::bytea,NULL::bytea,NULL::text,NULL::text,NULL::timestamptz; RETURN; END IF;
 IF m.status IN ('sent','failed','expired') THEN RETURN QUERY SELECT 'terminal',NULL::bigint,NULL::bytea,NULL::bytea,NULL::text,NULL::text,NULL::timestamptz; RETURN; END IF;
 IF m.expires_at<=clock_timestamp() THEN
  UPDATE photo.mail_payloads SET status='expired',recipient_cipher=''::bytea,payload_cipher=''::bytea,lease_until=NULL WHERE id=m.id;
  RETURN QUERY SELECT 'terminal',NULL::bigint,NULL::bytea,NULL::bytea,NULL::text,NULL::text,NULL::timestamptz; RETURN;
 END IF;
 IF m.status='sending' THEN RETURN QUERY SELECT 'busy',NULL::bigint,NULL::bytea,NULL::bytea,NULL::text,NULL::text,NULL::timestamptz; RETURN; END IF;
 IF p_attempt<>m.attempt+1 OR m.next_attempt_at>clock_timestamp() THEN RETURN QUERY SELECT 'stale',NULL::bigint,NULL::bytea,NULL::bytea,NULL::text,NULL::text,NULL::timestamptz; RETURN; END IF;
 UPDATE photo.mail_payloads SET status='sending',attempt=p_attempt,fencing_token=fencing_token+1,worker_id=p_worker,lease_until=least(clock_timestamp()+interval '60 seconds',m.expires_at) WHERE id=m.id;
 RETURN QUERY SELECT 'claimed',m.fencing_token+1,m.recipient_cipher,m.payload_cipher,m.key_id,m.message_id,m.expires_at;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_mail(uuid,int,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_mail(uuid,int,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_mail(uuid,int,uuid) TO photo_mail;
-- +goose StatementBegin
CREATE FUNCTION photo.finish_mail(p_payload uuid,p_fence bigint,p_worker uuid,p_sent boolean,p_retryable boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m photo.mail_payloads%ROWTYPE; next_time timestamptz;
BEGIN
 SELECT * INTO m FROM photo.mail_payloads WHERE id=p_payload FOR UPDATE;
 IF NOT FOUND OR m.fencing_token<>p_fence OR m.worker_id<>p_worker THEN RETURN false; END IF;
 IF m.status='sent' AND p_sent THEN RETURN true; END IF;
 IF m.status<>'sending' OR m.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 IF p_sent THEN
  UPDATE photo.mail_payloads SET status='sent',recipient_cipher=''::bytea,payload_cipher=''::bytea,lease_until=NULL WHERE id=m.id;
 ELSIF p_retryable AND m.attempt<4 AND m.expires_at>clock_timestamp()+interval '5 seconds' THEN
  next_time:=clock_timestamp()+make_interval(secs=>CASE m.attempt WHEN 1 THEN 5 WHEN 2 THEN 30 ELSE 120 END);
  UPDATE photo.mail_payloads SET status='retry',lease_until=NULL,next_attempt_at=next_time WHERE id=m.id;
 ELSE
  UPDATE photo.mail_payloads SET status='failed',recipient_cipher=''::bytea,payload_cipher=''::bytea,lease_until=NULL WHERE id=m.id;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_mail(uuid,bigint,uuid,boolean,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_mail(uuid,bigint,uuid,boolean,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_mail(uuid,bigint,uuid,boolean,boolean) TO photo_mail;
-- +goose StatementBegin
CREATE FUNCTION photo.reconcile_mail(p_limit int) RETURNS int LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE m photo.mail_payloads%ROWTYPE; count_rows int:=0;
BEGIN
 IF p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'invalid mail sweep limit'; END IF;
 FOR m IN SELECT * FROM photo.mail_payloads WHERE status IN ('queued','retry','sending') AND
 (expires_at<=clock_timestamp() OR (status='sending' AND lease_until<=clock_timestamp()) OR (status='retry' AND next_attempt_at<=clock_timestamp()))
 ORDER BY created_at,id LIMIT p_limit FOR UPDATE SKIP LOCKED LOOP
  count_rows:=count_rows+1;
  IF m.expires_at<=clock_timestamp() OR m.attempt>=4 THEN
   UPDATE photo.mail_payloads SET status=CASE WHEN m.expires_at<=clock_timestamp() THEN 'expired' ELSE 'failed' END,recipient_cipher=''::bytea,payload_cipher=''::bytea,lease_until=NULL,fencing_token=fencing_token+1 WHERE id=m.id;
  ELSE
   UPDATE photo.mail_payloads SET status='queued',next_attempt_at=NULL,lease_until=NULL,fencing_token=fencing_token+1 WHERE id=m.id;
   INSERT INTO photo.auth_mail_outbox(event_id,payload_id) VALUES(gen_random_uuid(),m.id);
  END IF;
 END LOOP; RETURN count_rows;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reconcile_mail(int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reconcile_mail(int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reconcile_mail(int) TO photo_maintenance,photo_mail;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
