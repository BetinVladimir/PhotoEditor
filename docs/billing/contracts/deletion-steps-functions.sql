-- Durable deletion worker protocol. No external calls or destructive effects.
CREATE FUNCTION photo.account_enqueue_deletion_step(p_request uuid,p_step_key text,p_ordinal bigint,p_input_hash bytea) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_id uuid; v_old photo.profile_deletion_steps%ROWTYPE;
BEGIN
 IF p_request IS NULL OR p_step_key IS NULL OR p_step_key !~ '^[a-z][a-z0-9_]{0,63}$'
 OR p_ordinal IS NULL OR p_ordinal<1 OR p_input_hash IS NULL OR octet_length(p_input_hash)<>32
 THEN RAISE EXCEPTION 'DELETION_STEP_INPUT_INVALID';END IF;
 PERFORM 1 FROM profile_deletion_requests WHERE request_id=p_request
 AND state IN ('erasure_requested','revoking','purging','external_pending') FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELETION_REQUEST_NOT_ACTIVE';END IF;
 SELECT * INTO v_old FROM profile_deletion_steps WHERE request_id=p_request AND step_key=p_step_key FOR UPDATE;
 IF FOUND THEN
  IF v_old.input_hash IS DISTINCT FROM p_input_hash OR v_old.ordinal IS DISTINCT FROM p_ordinal
  THEN RAISE EXCEPTION 'DELETION_STEP_HASH_CONFLICT';END IF;
  RETURN v_old.step_id;
 END IF;
 INSERT INTO profile_deletion_steps(request_id,step_key,ordinal,input_hash)
 VALUES(p_request,p_step_key,p_ordinal,p_input_hash) ON CONFLICT(request_id,step_key) DO NOTHING
 RETURNING step_id INTO v_id;
 IF v_id IS NULL THEN
  SELECT * INTO v_old FROM profile_deletion_steps WHERE request_id=p_request AND step_key=p_step_key FOR UPDATE;
  IF v_old.input_hash IS DISTINCT FROM p_input_hash OR v_old.ordinal IS DISTINCT FROM p_ordinal
  THEN RAISE EXCEPTION 'DELETION_STEP_HASH_CONFLICT';END IF;
  v_id:=v_old.step_id;
 END IF;
 RETURN v_id;
END $$;

CREATE FUNCTION photo.account_claim_deletion_step(p_worker uuid,p_batch_size bigint,p_lease_seconds bigint) RETURNS TABLE(step_id uuid,request_id uuid,step_key text,fence uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item record; token uuid; deadline timestamptz;
BEGIN
 IF p_worker IS NULL OR p_batch_size IS NULL OR p_batch_size<1 OR p_batch_size>100
 OR p_lease_seconds IS NULL OR p_lease_seconds<1 OR p_lease_seconds>3600
 THEN RAISE EXCEPTION 'DELETION_CLAIM_INPUT_INVALID';END IF;
 FOR item IN
  SELECT s.step_id,s.request_id,s.step_key FROM profile_deletion_steps s
  JOIN profile_deletion_requests r ON r.request_id=s.request_id
  WHERE r.state IN ('erasure_requested','revoking','purging','external_pending')
  AND ((s.state IN ('pending','retry') AND s.next_retry_at<=clock_timestamp())
    OR (s.state='processing' AND s.lease_until<=clock_timestamp()))
  AND NOT EXISTS(SELECT 1 FROM profile_deletion_steps earlier WHERE earlier.request_id=s.request_id
    AND earlier.ordinal<s.ordinal AND earlier.state<>'completed')
  ORDER BY s.request_id,s.ordinal LIMIT p_batch_size
  FOR SHARE OF r SKIP LOCKED FOR UPDATE OF s SKIP LOCKED
 LOOP
  token:=gen_random_uuid();deadline:=clock_timestamp()+make_interval(secs=>p_lease_seconds::integer);
  UPDATE profile_deletion_steps s SET state='processing',worker_id=p_worker,lease_token=token,
   lease_until=deadline,finish_hash=NULL,attempts=s.attempts+1,updated_at=clock_timestamp() WHERE s.step_id=item.step_id;
  step_id:=item.step_id;request_id:=item.request_id;step_key:=item.step_key;fence:=token;lease_until:=deadline;
  RETURN NEXT;
 END LOOP;
END $$;

CREATE FUNCTION photo.account_record_deletion_step(p_step uuid,p_worker uuid,p_fence uuid,p_outcome text,p_result_hash bytea,p_error_code text,p_retry_seconds bigint) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE old photo.profile_deletion_steps%ROWTYPE; parent_id uuid; parent_state text; fingerprint bytea;
BEGIN
 IF p_step IS NULL OR p_worker IS NULL OR p_fence IS NULL OR p_outcome IS NULL
 OR p_outcome NOT IN ('completed','retry','failed')
 OR (p_outcome='completed' AND (p_result_hash IS NULL OR octet_length(p_result_hash)<>32 OR p_error_code IS NOT NULL))
 OR (p_outcome<>'completed' AND (p_result_hash IS NOT NULL OR p_error_code IS NULL OR p_error_code !~ '^[A-Z][A-Z0-9_]{0,63}$'))
 OR (p_outcome='retry' AND (p_retry_seconds IS NULL OR p_retry_seconds<1 OR p_retry_seconds>86400))
 OR (p_outcome<>'retry' AND p_retry_seconds IS NOT NULL)
 THEN RAISE EXCEPTION 'DELETION_RESULT_INPUT_INVALID';END IF;
 fingerprint:=sha256(convert_to(jsonb_build_array(p_outcome,encode(p_result_hash,'hex'),p_error_code,p_retry_seconds)::text,'UTF8'));
 SELECT s.request_id INTO parent_id FROM profile_deletion_steps s WHERE s.step_id=p_step;
 SELECT state INTO parent_state FROM profile_deletion_requests WHERE request_id=parent_id FOR SHARE;
 SELECT * INTO old FROM profile_deletion_steps WHERE step_id=p_step FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'DELETION_STEP_NOT_FOUND';END IF;
 IF old.worker_id IS DISTINCT FROM p_worker OR old.lease_token IS DISTINCT FROM p_fence
 THEN RAISE EXCEPTION 'DELETION_FENCE_STALE';END IF;
 IF old.state IN ('completed','retry','failed') THEN
  IF old.finish_hash IS NOT DISTINCT FROM fingerprint THEN RETURN old.state;END IF;
  RAISE EXCEPTION 'DELETION_STEP_HASH_CONFLICT';
 END IF;
 IF parent_state NOT IN ('erasure_requested','revoking','purging','external_pending')
 THEN RAISE EXCEPTION 'DELETION_REQUEST_NOT_ACTIVE';END IF;
 IF old.state<>'processing' OR old.lease_until<=clock_timestamp()
 THEN RAISE EXCEPTION 'DELETION_FENCE_STALE';END IF;
 UPDATE profile_deletion_steps SET state=p_outcome,result_hash=p_result_hash,last_error_code=p_error_code,finish_hash=fingerprint,
  next_retry_at=CASE WHEN p_outcome='retry' THEN clock_timestamp()+make_interval(secs=>p_retry_seconds::integer) ELSE next_retry_at END,
  lease_until=NULL,updated_at=clock_timestamp() WHERE step_id=p_step;
 RETURN p_outcome;
END $$;
