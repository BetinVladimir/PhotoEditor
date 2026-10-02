-- +goose Up
ALTER TABLE photo.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.tenants FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.tenants TO photo_api USING(id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.tenants TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,UPDATE ON photo.tenants TO photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.authorize_tenant(p_user uuid,p_session uuid,p_tenant uuid)
RETURNS TABLE(role text,revision bigint) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT m.role,t.revision FROM photo.memberships m JOIN photo.tenants t ON t.id=m.tenant_id
 JOIN photo.users u ON u.id=m.user_id JOIN photo.sessions s ON s.user_id=u.id
 WHERE m.user_id=p_user AND s.id=p_session AND m.tenant_id=p_tenant AND m.status='active'
 AND t.status='active' AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>now()
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.authorize_tenant(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_tenant(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.authorize_tenant(uuid,uuid,uuid) TO photo_api;
-- +goose StatementBegin
CREATE FUNCTION photo.list_user_tenants(p_user uuid,p_session uuid,p_after uuid,p_limit int)
RETURNS TABLE(id uuid,name text,role text,revision bigint) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.id,t.name,m.role,t.revision FROM photo.tenants t JOIN photo.memberships m ON m.tenant_id=t.id
 JOIN photo.users u ON u.id=m.user_id JOIN photo.sessions s ON s.user_id=u.id
 WHERE m.user_id=p_user AND s.id=p_session AND m.status='active' AND t.status='active' AND u.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>now() AND (p_after IS NULL OR t.id>p_after)
 ORDER BY t.id LIMIT least(greatest(p_limit,1),101)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_user_tenants(uuid,uuid,uuid,int) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_user_tenants(uuid,uuid,uuid,int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.list_user_tenants(uuid,uuid,uuid,int) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
