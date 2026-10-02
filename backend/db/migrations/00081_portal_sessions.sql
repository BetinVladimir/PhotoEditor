-- +goose Up
-- Portal traffic is function-only. Runtime role receives no table grants and
-- cannot derive a tenant or collection from client-controlled identifiers.
-- +goose StatementBegin
CREATE FUNCTION photo.exchange_share_session(p_share uuid,p_secret_hash bytea,p_session_hash bytea,p_csrf_hash bytea,p_locale text,p_expires timestamptz)
RETURNS TABLE(session_id uuid,tenant_id uuid,collection_id uuid,link_version bigint,expires_at timestamptz,permissions jsonb,allowed_presets jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
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
ALTER FUNCTION photo.exchange_share_session(uuid,bytea,bytea,bytea,text,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.exchange_share_session(uuid,bytea,bytea,bytea,text,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.exchange_share_session(uuid,bytea,bytea,bytea,text,timestamptz) TO photo_portal;

-- Every portal operation re-resolves the opaque cookie and current link
-- revision. Revocation or policy change invalidates existing sessions at once.
-- +goose StatementBegin
CREATE FUNCTION photo.authorize_portal_session(p_session_hash bytea,p_csrf_hash bytea,p_permission text)
RETURNS TABLE(session_id uuid,tenant_id uuid,share_id uuid,collection_id uuid,link_version bigint,locale text,permissions jsonb,limits jsonb,moderation text,allowed_presets jsonb)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
BEGIN
 IF octet_length(p_session_hash)<>32 OR p_permission NOT IN ('session','view','download','upload')
 OR (p_csrf_hash IS NOT NULL AND octet_length(p_csrf_hash)<>32) THEN RETURN;END IF;
 RETURN QUERY SELECT s.id,s.tenant_id,s.share_id,s.collection_id,s.link_version,s.locale,l.permissions,l.limits,l.moderation,l.allowed_presets
 FROM photo.share_sessions s JOIN photo.share_links l ON l.tenant_id=s.tenant_id AND l.id=s.share_id AND l.collection_id=s.collection_id
 JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id
 WHERE s.session_hash=p_session_hash AND (p_csrf_hash IS NULL OR s.csrf_hash=p_csrf_hash)
 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND l.revoked_at IS NULL
 AND (l.expires_at IS NULL OR l.expires_at>clock_timestamp()) AND l.policy_version=s.link_version
 AND (p_permission='session' OR l.permissions->p_permission='true'::jsonb) AND c.deleted_at IS NULL AND c.state<>'archived';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.authorize_portal_session(bytea,bytea,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_portal_session(bytea,bytea,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_portal_session(bytea,bytea,text) TO photo_portal;

-- Sanitized collection projection. Upload-only shares receive no cover,
-- sections or item-derived information.
-- +goose StatementBegin
CREATE FUNCTION photo.get_portal_collection(p_session_hash bytea)
RETURNS TABLE(name text,description text,revision bigint,cover jsonb,capabilities jsonb,allowed_presets jsonb,sections jsonb,empty boolean)
LANGUAGE plpgsql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
DECLARE s photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;c photo.collections%ROWTYPE;can_view boolean;
BEGIN
 IF octet_length(p_session_hash)<>32 THEN RETURN;END IF;
 SELECT * INTO s FROM photo.share_sessions WHERE session_hash=p_session_hash;
 IF NOT FOUND OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp() THEN RETURN;END IF;
 SELECT * INTO l FROM photo.share_links WHERE tenant_id=s.tenant_id AND id=s.share_id AND collection_id=s.collection_id;
 IF NOT FOUND OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR l.policy_version<>s.link_version THEN RETURN;END IF;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=s.tenant_id AND id=s.collection_id AND deleted_at IS NULL AND state<>'archived';
 IF NOT FOUND THEN RETURN;END IF;
 can_view:=c.state='published' AND l.permissions->'view'='true'::jsonb;
 RETURN QUERY SELECT c.name,c.description,c.revision,
  CASE WHEN can_view AND c.cover_item_id IS NOT NULL AND EXISTS(SELECT FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published' WHERE i.tenant_id=c.tenant_id AND i.id=c.cover_item_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted') THEN jsonb_build_object('item_id',c.cover_item_id) ELSE NULL::jsonb END,
  jsonb_build_object('view',can_view,'download',can_view AND l.permissions->'download'='true'::jsonb,'upload',l.permissions->'upload'='true'::jsonb),
  l.allowed_presets,
  CASE WHEN can_view THEN coalesce((SELECT jsonb_agg(jsonb_build_object('id',x.id,'name',x.name,'position',x.position,'revision',x.revision) ORDER BY x.position,x.id) FROM photo.sections x WHERE x.tenant_id=c.tenant_id AND x.collection_id=c.id),'[]'::jsonb) ELSE '[]'::jsonb END,
  NOT can_view OR NOT EXISTS(SELECT FROM photo.collection_items i JOIN photo.publications p ON p.tenant_id=i.tenant_id AND p.item_id=i.id AND p.asset_version_id=i.asset_version_id AND p.state='published' WHERE i.tenant_id=c.tenant_id AND i.collection_id=c.id AND i.visibility='visible' AND i.moderation_state='accepted');
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_portal_collection(bytea) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_portal_collection(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.get_portal_collection(bytea) TO photo_portal;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
