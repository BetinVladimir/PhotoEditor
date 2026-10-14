-- +goose Up
-- The second all-bucket absence scan must retain the same fenced deletion job.
-- A stale worker cannot renew an expired or replaced cleanup token.
-- +goose StatementBegin
CREATE FUNCTION photo.renew_tenant_deletion_lease(p_job uuid,p_token uuid,p_tenant uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.deletion_jobs j
 SET cleanup_lease_until=clock_timestamp()+interval '60 seconds'
 WHERE j.id=p_job AND j.resource_type='tenant' AND j.resource_id=p_tenant
  AND j.tenant_id=p_tenant AND j.status='running' AND j.cleanup_token=p_token
  AND j.cleanup_lease_until>clock_timestamp()
  AND EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='deleting');
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_tenant_deletion_lease(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_tenant_deletion_lease(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.renew_tenant_deletion_lease(uuid,uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
