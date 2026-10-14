-- +goose Up
-- Stable event identity/time for independent backup before object cleanup.
-- No application table grants and no deletion lifecycle changes.
-- +goose StatementBegin
CREATE FUNCTION photo.v32_tenant_deletion_history_intent(p_job uuid,p_token uuid,p_tenant uuid)
RETURNS TABLE(requested_at timestamptz)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT j.created_at FROM photo.deletion_jobs j
 JOIN photo.tenants t ON t.id=j.tenant_id AND t.status='deleting'
 WHERE j.id=p_job AND j.tenant_id=p_tenant AND j.resource_type='tenant'
 AND j.resource_id=p_tenant AND j.status='running' AND j.cleanup_token=p_token
 AND j.cleanup_lease_until>clock_timestamp()
$$;
ALTER FUNCTION photo.v32_tenant_deletion_history_intent(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v32_tenant_deletion_history_intent(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v32_tenant_deletion_history_intent(uuid,uuid,uuid) TO photo_maintenance;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
