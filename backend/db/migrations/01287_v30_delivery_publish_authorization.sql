-- +goose Up
-- Replayed response still requires current project editor authority.
-- +goose StatementBegin
CREATE FUNCTION photo.authorize_v30_delivery_publish(p_tenant uuid,p_package uuid,p_actor uuid,p_session uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce((SELECT photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session)
 FROM photo.delivery_packages d WHERE d.tenant_id=p_tenant AND d.id=p_package),false)
$$;
ALTER FUNCTION photo.authorize_v30_delivery_publish(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_v30_delivery_publish(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.authorize_v30_delivery_publish(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
