-- One-use capture challenges and shared public-endpoint rate limits.
-- +goose Up
GRANT SELECT,INSERT,UPDATE ON photo.auth_rate_limits TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.capture_challenge_rate(p_ip bytea,p_device bytea)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE minute timestamptz:=date_trunc('minute',clock_timestamp());
 ip_count integer; device_count integer;
BEGIN
 IF octet_length(p_ip)<>32 OR octet_length(p_device)<>32 THEN
  RAISE EXCEPTION 'V24_RATE_INVALID' USING ERRCODE='22023'; END IF;
 INSERT INTO photo.auth_rate_limits(scope_hash,window_start,count,expires_at)
 VALUES(p_ip,minute,1,minute+interval '2 minutes')
 ON CONFLICT(scope_hash,window_start) DO UPDATE
  SET count=LEAST(photo.auth_rate_limits.count+1,1000000)
 RETURNING count INTO ip_count;
 INSERT INTO photo.auth_rate_limits(scope_hash,window_start,count,expires_at)
 VALUES(p_device,minute,1,minute+interval '2 minutes')
 ON CONFLICT(scope_hash,window_start) DO UPDATE
  SET count=LEAST(photo.auth_rate_limits.count+1,1000000)
 RETURNING count INTO device_count;
 RETURN ip_count<=30 AND device_count<=5;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_challenge_rate(bytea,bytea) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_challenge_rate(bytea,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_challenge_rate(bytea,bytea) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.capture_token_rate(p_ip bytea,p_device bytea)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE minute timestamptz:=date_trunc('minute',clock_timestamp());
 ip_count integer; device_count integer;
BEGIN
 IF octet_length(p_ip)<>32 OR octet_length(p_device)<>32 THEN
  RAISE EXCEPTION 'V24_RATE_INVALID' USING ERRCODE='22023'; END IF;
 INSERT INTO photo.auth_rate_limits(scope_hash,window_start,count,expires_at)
 VALUES(p_ip,minute,1,minute+interval '2 minutes')
 ON CONFLICT(scope_hash,window_start) DO UPDATE
  SET count=LEAST(photo.auth_rate_limits.count+1,1000000)
 RETURNING count INTO ip_count;
 INSERT INTO photo.auth_rate_limits(scope_hash,window_start,count,expires_at)
 VALUES(p_device,minute,1,minute+interval '2 minutes')
 ON CONFLICT(scope_hash,window_start) DO UPDATE
  SET count=LEAST(photo.auth_rate_limits.count+1,1000000)
 RETURNING count INTO device_count;
 RETURN ip_count<=30 AND device_count<=10;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_token_rate(bytea,bytea) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_token_rate(bytea,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_token_rate(bytea,bytea) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.capture_challenge_target(p_tenant uuid,p_device uuid)
RETURNS TABLE(project_id uuid,public_key text,credential_epoch bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT d.project_id,d.public_key,d.credential_epoch
 FROM photo.capture_devices d JOIN photo.tenants t ON t.id=d.tenant_id
 JOIN photo.projects p ON p.tenant_id=d.tenant_id AND p.id=d.project_id
 WHERE d.tenant_id=p_tenant AND d.id=p_device AND d.state='active'
  AND t.status='active' AND p.archived_at IS NULL
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_challenge_target(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_challenge_target(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_challenge_target(uuid,uuid) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.create_capture_challenge(p_tenant uuid,p_device uuid,
 p_challenge uuid,p_epoch bigint,p_sealed bytea,p_key_version text,p_hash text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.capture_devices%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_device IS NULL OR p_challenge IS NULL OR p_epoch<1 OR
  octet_length(p_sealed) NOT BETWEEN 48 AND 256 OR
  length(coalesce(p_key_version,'')) NOT BETWEEN 1 AND 256 OR
  coalesce(p_hash,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V24_CHALLENGE_INVALID' USING ERRCODE='22023'; END IF;
 SELECT * INTO d FROM photo.capture_devices WHERE tenant_id=p_tenant AND id=p_device
  FOR UPDATE;
 IF NOT FOUND OR d.state<>'active' OR d.credential_epoch<>p_epoch OR
  NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') OR
  NOT EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=p_tenant
   AND p.id=d.project_id AND p.archived_at IS NULL) THEN RETURN false; END IF;
 INSERT INTO photo.capture_challenges(tenant_id,project_id,id,device_id,
  nonce_ciphertext,key_version,nonce_hash,credential_epoch,expires_at)
 VALUES(p_tenant,d.project_id,p_challenge,p_device,p_sealed,p_key_version,p_hash,
  p_epoch,clock_timestamp()+interval '60 seconds');
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.create_capture_challenge(uuid,uuid,uuid,bigint,bytea,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.create_capture_challenge(uuid,uuid,uuid,bigint,bytea,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.create_capture_challenge(uuid,uuid,uuid,bigint,bytea,text,text) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.capture_challenge_payload(p_tenant uuid,p_device uuid,p_challenge uuid)
RETURNS TABLE(project_id uuid,public_key text,credential_epoch bigint,
 key_version text,nonce_ciphertext bytea,nonce_hash text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT d.project_id,d.public_key,c.credential_epoch,c.key_version,
  c.nonce_ciphertext,c.nonce_hash
 FROM photo.capture_devices d JOIN photo.capture_challenges c
  ON c.tenant_id=d.tenant_id AND c.project_id=d.project_id AND c.device_id=d.id
 JOIN photo.tenants t ON t.id=d.tenant_id
 JOIN photo.projects p ON p.tenant_id=d.tenant_id AND p.id=d.project_id
 WHERE d.tenant_id=p_tenant AND d.id=p_device AND c.id=p_challenge
  AND d.state='active' AND d.credential_epoch=c.credential_epoch
  AND c.consumed_at IS NULL AND c.expires_at>clock_timestamp()
  AND t.status='active' AND p.archived_at IS NULL
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_challenge_payload(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_challenge_payload(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_challenge_payload(uuid,uuid,uuid) TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.consume_capture_challenge(p_tenant uuid,p_device uuid,
 p_challenge uuid,p_epoch bigint,p_nonce_hash text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.capture_devices%ROWTYPE; c photo.capture_challenges%ROWTYPE;
BEGIN
 SELECT * INTO d FROM photo.capture_devices WHERE tenant_id=p_tenant AND id=p_device
  FOR UPDATE;
 IF NOT FOUND OR d.state<>'active' OR d.credential_epoch<>p_epoch THEN RETURN false; END IF;
 SELECT * INTO c FROM photo.capture_challenges WHERE tenant_id=p_tenant
  AND device_id=p_device AND id=p_challenge FOR UPDATE;
 IF NOT FOUND OR c.project_id<>d.project_id OR c.credential_epoch<>p_epoch OR
  c.nonce_hash<>p_nonce_hash OR c.consumed_at IS NOT NULL OR
  c.expires_at<=clock_timestamp() OR
  NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') OR
  NOT EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=p_tenant
   AND p.id=d.project_id AND p.archived_at IS NULL) THEN RETURN false; END IF;
 UPDATE photo.capture_challenges SET consumed_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=p_challenge;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.consume_capture_challenge(uuid,uuid,uuid,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.consume_capture_challenge(uuid,uuid,uuid,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.consume_capture_challenge(uuid,uuid,uuid,bigint,text) TO photo_api;

-- Every device-scoped mutation rechecks the current row on the primary DB.
-- A still-valid JWT cannot outlive revocation or credential rotation.
-- +goose StatementBegin
CREATE FUNCTION photo.capture_device_principal(p_tenant uuid,p_project uuid,
 p_device uuid,p_epoch bigint) RETURNS boolean
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.capture_devices d
  JOIN photo.tenants t ON t.id=d.tenant_id
  JOIN photo.projects p ON p.tenant_id=d.tenant_id AND p.id=d.project_id
  WHERE d.tenant_id=p_tenant AND d.project_id=p_project AND d.id=p_device
   AND d.credential_epoch=p_epoch AND d.state='active'
   AND t.status='active' AND p.archived_at IS NULL)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_device_principal(uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_device_principal(uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_device_principal(uuid,uuid,uuid,bigint) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
