-- +goose Up
-- Receipt waits must not extend the session lifetime to transaction start time.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.authorize_tenant(p_user uuid,p_session uuid,p_tenant uuid)
RETURNS TABLE(role text,revision bigint) LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT m.role,t.revision FROM photo.memberships m JOIN photo.tenants t ON t.id=m.tenant_id
 JOIN photo.users u ON u.id=m.user_id JOIN photo.sessions s ON s.user_id=u.id
 WHERE m.user_id=p_user AND s.id=p_session AND m.tenant_id=p_tenant AND m.status='active'
 AND t.status='active' AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.authorize_tenant(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_tenant(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_tenant(uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
