CREATE OR REPLACE FUNCTION photo.account_issue_recent_auth(
 p_tenant uuid,p_actor uuid,p_session uuid,
 p_purpose text,
 p_otp_challenge_ref text
) RETURNS TABLE(grant_id uuid,proof_hash bytea,expires_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF nullif(current_setting('app.tenant_id',true),'')::uuid IS DISTINCT FROM p_tenant
 THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant))
 THEN RAISE EXCEPTION 'BILLING_AUTHORITY_INVALID';END IF;
 IF p_purpose IS NULL OR p_purpose NOT IN ('delete_profile','resolve_ownership')
 THEN RAISE EXCEPTION 'LEGAL_PURPOSE_INVALID';END IF;
 -- A caller-supplied challenge reference is not verified fresh authentication.
 -- Remain closed until the purpose/session/epoch-bound OTP verifier is installed.
 RAISE EXCEPTION 'LEGAL_RECENT_AUTH_VERIFIER_REQUIRED';
END $$;
