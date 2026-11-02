CREATE FUNCTION photo.billing_ingest_stripe_webhook(p_inbox_id uuid,p_account uuid,p_external_account text,p_api_version text,p_event_id text,p_event_type text,p_object_id text,p_provider_created_at timestamptz,p_payload_encrypted bytea,p_payload_hash bytea)
RETURNS TABLE(inbox_id uuid,created boolean,quarantined boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item photo.billing_webhook_inbox%ROWTYPE; inserted_id uuid;
BEGIN
 IF p_inbox_id IS NULL OR p_account IS NULL OR p_external_account IS NULL OR p_api_version IS NULL OR p_event_id IS NULL OR p_event_id !~ '^evt_[A-Za-z0-9]+$'
 OR p_event_type IS NULL OR length(p_event_type) NOT BETWEEN 1 AND 200 OR p_object_id IS NULL OR length(p_object_id) NOT BETWEEN 1 AND 255
 OR p_provider_created_at IS NULL OR NOT isfinite(p_provider_created_at) OR p_payload_encrypted IS NULL OR octet_length(p_payload_encrypted)=0
 OR p_payload_hash IS NULL OR octet_length(p_payload_hash)<>32
 THEN RAISE EXCEPTION 'BILLING_WEBHOOK_INPUT_INCOMPLETE';END IF;
 PERFORM 1 FROM photo.billing_provider_accounts a WHERE a.id=p_account AND a.external_account_id=p_external_account AND a.provider='stripe' AND a.environment='test'
 AND a.catalog_scope='sandbox' AND a.webhook_format='snapshot' AND a.api_version=p_api_version;
 IF NOT FOUND THEN RAISE EXCEPTION 'BILLING_WEBHOOK_INPUT_INCOMPLETE';END IF;
 INSERT INTO photo.billing_webhook_inbox(id,provider_account_id,event_id,event_type,object_id,provider_created_at,payload_encrypted,payload_hash,received_api_version)
 VALUES(p_inbox_id,p_account,p_event_id,p_event_type,p_object_id,p_provider_created_at,p_payload_encrypted,p_payload_hash,p_api_version)
 ON CONFLICT(provider_account_id,event_id) DO NOTHING RETURNING id INTO inserted_id;
 IF inserted_id IS NOT NULL THEN RETURN QUERY SELECT inserted_id,true,false;RETURN;END IF;
 SELECT * INTO item FROM photo.billing_webhook_inbox w WHERE w.provider_account_id=p_account AND w.event_id=p_event_id FOR UPDATE;
 IF item.payload_hash IS DISTINCT FROM p_payload_hash OR item.event_type IS DISTINCT FROM p_event_type
 OR item.object_id IS DISTINCT FROM p_object_id OR item.provider_created_at IS DISTINCT FROM p_provider_created_at
 OR item.received_api_version IS DISTINCT FROM p_api_version THEN
  INSERT INTO photo.billing_webhook_conflicts(inbox_id,payload_hash,event_type,object_id)
  VALUES(item.id,p_payload_hash,p_event_type,p_object_id) ON CONFLICT ON CONSTRAINT stripe_conflict_identity DO NOTHING;
  UPDATE photo.billing_webhook_inbox w SET state='dead',last_error_code='WEBHOOK_HASH_CONFLICT',processing_token=NULL,lease_until=NULL
  WHERE w.id=item.id AND w.state<>'applied';
  RETURN QUERY SELECT item.id,false,true;RETURN;
 END IF;
 RETURN QUERY SELECT item.id,false,false;
END $$;


CREATE FUNCTION photo.billing_stripe_ingress_ready(p_account uuid,p_external_account text,p_api_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN EXISTS(SELECT 1 FROM photo.billing_provider_accounts a WHERE a.id=p_account AND a.external_account_id=p_external_account
 AND a.provider='stripe' AND a.environment='test' AND a.catalog_scope='sandbox' AND a.webhook_format='snapshot' AND a.api_version=p_api_version);
END $$;
