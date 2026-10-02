-- +goose Up
-- Trusted bootstrap is the only auth-role path into tenant-owned data.
-- +goose StatementBegin
CREATE FUNCTION photo.auth_bootstrap_user(p_email text,p_locale text,p_registration boolean)
RETURNS TABLE(user_id uuid,auth_epoch bigint) LANGUAGE plpgsql SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
DECLARE uid uuid; tid uuid; epoch bigint; state text;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('auth-user:'||p_email,0));
  SELECT u.id,u.auth_epoch,u.status INTO uid,epoch,state FROM photo.users u WHERE u.email_normalized=p_email FOR UPDATE;
  IF uid IS NOT NULL THEN
    IF state<>'active' THEN RETURN; END IF;
    RETURN QUERY SELECT uid,epoch; RETURN;
  END IF;
  IF NOT p_registration THEN RETURN; END IF;
  IF p_locale NOT IN ('ru','bg','en') OR length(p_email)>254 THEN RAISE EXCEPTION 'invalid identity' USING ERRCODE='22023'; END IF;
  INSERT INTO photo.users(email_normalized,locale) VALUES(p_email,p_locale) RETURNING id INTO uid;
  INSERT INTO photo.tenants(name,owner_user_id,quotas,retention) VALUES('Personal',uid,
    '{"storage_bytes":10737418240,"source_files":10000,"active_runs":4,"active_gpu_tasks":1}',
    '{"deleted_days":30,"artifact_days":30}') RETURNING id INTO tid;
  INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(tid,uid,'owner');
  INSERT INTO photo.tenant_usage(tenant_id) VALUES(tid);
  RETURN QUERY SELECT uid,0::bigint;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.auth_bootstrap_user(text,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.auth_bootstrap_user(text,text,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.auth_bootstrap_user(text,text,boolean) TO photo_auth;
CREATE INDEX sessions_family_idx ON photo.sessions(family_id);
CREATE INDEX auth_challenges_resend_idx ON photo.auth_challenges(email_normalized,created_at DESC);
CREATE INDEX mail_payloads_ready_idx ON photo.mail_payloads(status,next_attempt_at) WHERE status IN ('queued','retry');
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
