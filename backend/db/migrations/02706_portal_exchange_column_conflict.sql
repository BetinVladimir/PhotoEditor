-- +goose Up
-- 00081 declared exchange_share_session with RETURNS TABLE(tenant_id, collection_id, expires_at, ...) and then used the same
-- bare names as columns (WHERE tenant_id=l.tenant_id), so every exchange of an existing share failed with
-- "column reference \"tenant_id\" is ambiguous". Same body, columns win over the output variables. Owner and grants are kept
-- by CREATE OR REPLACE.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.exchange_share_session(p_share uuid,p_secret_hash bytea,p_session_hash bytea,p_csrf_hash bytea,p_locale text,p_expires timestamptz)
RETURNS TABLE(session_id uuid,tenant_id uuid,collection_id uuid,link_version bigint,expires_at timestamptz,permissions jsonb,allowed_presets jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
#variable_conflict use_column
DECLARE l photo.share_links%ROWTYPE;c photo.collections%ROWTYPE;sid uuid:=gen_random_uuid();until_time timestamptz;
BEGIN
 IF p_share IS NULL OR octet_length(p_secret_hash)<>32 OR octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32
 OR p_locale NOT IN ('ru','bg','en') OR p_expires IS NULL OR p_expires<=clock_timestamp() OR p_expires>clock_timestamp()+interval '24 hours' THEN RETURN;END IF;
 SELECT * INTO l FROM photo.share_links WHERE id=p_share AND secret_hash=p_secret_hash FOR UPDATE;
 IF NOT FOUND OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp())
 OR l.permissions->'view' IS NULL OR l.permissions->'download' IS NULL OR l.permissions->'upload' IS NULL THEN RETURN;END IF;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=l.tenant_id AND id=l.collection_id AND deleted_at IS NULL;
 IF NOT FOUND OR c.state='archived' THEN RETURN;END IF;
 until_time:=least(p_expires,coalesce(l.expires_at,p_expires));
 INSERT INTO photo.share_sessions(id,tenant_id,share_id,collection_id,session_hash,link_version,expires_at,locale,csrf_hash)
 VALUES(sid,l.tenant_id,l.id,l.collection_id,p_session_hash,l.policy_version,until_time,p_locale,p_csrf_hash);
 RETURN QUERY SELECT sid,l.tenant_id,l.collection_id,l.policy_version,until_time,l.permissions,l.allowed_presets;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
