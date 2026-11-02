CREATE FUNCTION photo.account_update_privacy_preferences(p_actor uuid,p_session uuid,p_epoch bigint,p_expected_revision bigint,p_analytics boolean,p_marketing boolean,p_key text,p_capture_channel text)
RETURNS TABLE(revision bigint,analytics boolean,marketing boolean,updated_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE prefs photo.user_privacy_preferences%ROWTYPE; prior photo.privacy_preference_requests%ROWTYPE;
 fingerprint bytea; old_analytics boolean;old_marketing boolean;latest_event uuid;capture_channel text;
BEGIN
 -- User-global preferences remain available before the first tenant exists.
 PERFORM 1 FROM photo.users u WHERE u.id=p_actor AND u.status='active' AND u.auth_epoch=p_epoch FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 SELECT s.client_type INTO capture_channel FROM photo.sessions s WHERE s.id=p_session AND s.user_id=p_actor
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() FOR SHARE NOWAIT;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 IF p_expected_revision IS NULL OR p_expected_revision<1 OR (p_analytics IS NULL AND p_marketing IS NULL)
 OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR capture_channel NOT IN ('web','native')
 OR (p_capture_channel IS NOT NULL AND p_capture_channel IS DISTINCT FROM capture_channel)
 THEN RAISE EXCEPTION 'PRIVACY_INPUT_INVALID';END IF;
 fingerprint:=sha256(convert_to(jsonb_build_array(p_expected_revision,p_analytics,p_marketing,capture_channel)::text,'UTF8'));
 INSERT INTO photo.user_privacy_preferences(user_id) VALUES(p_actor) ON CONFLICT(user_id) DO NOTHING;
 SELECT * INTO prefs FROM photo.user_privacy_preferences p WHERE p.user_id=p_actor FOR UPDATE;
 SELECT * INTO prior FROM photo.privacy_preference_requests r WHERE r.user_id=p_actor AND r.idempotency_key=p_key;
 IF FOUND THEN
  IF prior.request_hash IS DISTINCT FROM fingerprint THEN RAISE EXCEPTION 'PRIVACY_IDEMPOTENCY_CONFLICT';END IF;
  RETURN QUERY SELECT prior.response_revision,prior.response_analytics,prior.response_marketing,prior.response_updated_at;RETURN;
 END IF;
 IF prefs.revision<>p_expected_revision THEN RAISE EXCEPTION 'PRIVACY_REVISION_CONFLICT';END IF;
 old_analytics:=prefs.analytics;old_marketing:=prefs.marketing;
 prefs.analytics:=coalesce(p_analytics,prefs.analytics);prefs.marketing:=coalesce(p_marketing,prefs.marketing);
 IF prefs.analytics IS DISTINCT FROM old_analytics OR prefs.marketing IS DISTINCT FROM old_marketing THEN
  WITH inserted AS (
   INSERT INTO photo.account_legal_events(user_id,subject_ref,purpose,action,capture_channel,client_event_id,evidence_hash)
   SELECT p_actor,prefs.subject_ref,v.purpose,CASE WHEN v.new_value THEN 'granted' ELSE 'revoked' END,capture_channel,p_key,fingerprint
   FROM (VALUES('cookie_analytics',old_analytics,prefs.analytics),('marketing',old_marketing,prefs.marketing)) v(purpose,old_value,new_value)
   WHERE v.old_value IS DISTINCT FROM v.new_value RETURNING event_id
  ) SELECT event_id INTO latest_event FROM inserted ORDER BY event_id LIMIT 1;
  UPDATE photo.user_privacy_preferences p SET analytics=prefs.analytics,marketing=prefs.marketing,
   revision=p.revision+1,updated_at=clock_timestamp(),last_event_id=latest_event
   WHERE p.user_id=p_actor RETURNING * INTO prefs;
 END IF;
 INSERT INTO photo.privacy_preference_requests(user_id,idempotency_key,request_hash,response_revision,response_analytics,response_marketing,response_updated_at)
 VALUES(p_actor,p_key,fingerprint,prefs.revision,prefs.analytics,prefs.marketing,prefs.updated_at);
 RETURN QUERY SELECT prefs.revision,prefs.analytics,prefs.marketing,prefs.updated_at;
END $$;
