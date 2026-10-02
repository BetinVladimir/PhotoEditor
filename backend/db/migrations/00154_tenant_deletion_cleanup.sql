-- +goose Up
ALTER TABLE photo.deletion_jobs ADD COLUMN cleanup_token uuid;
ALTER TABLE photo.deletion_jobs ADD COLUMN cleanup_lease_until timestamptz;
ALTER TABLE photo.deletion_jobs ADD COLUMN cleanup_passes bigint NOT NULL DEFAULT 0 CHECK(cleanup_passes>=0);
-- +goose StatementBegin
CREATE FUNCTION photo.claim_tenant_deletion() RETURNS TABLE(job_id uuid,cleanup_token uuid,tenant_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;token uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO j FROM photo.deletion_jobs x JOIN photo.tenants t ON t.id=x.tenant_id WHERE x.resource_type='tenant' AND x.resource_id=x.tenant_id AND t.status='deleting' AND (x.status='queued' OR (x.status='running' AND x.cleanup_lease_until<=clock_timestamp())) AND NOT EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=x.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) ORDER BY x.created_at,x.id LIMIT 1 FOR UPDATE OF x SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.deletion_jobs SET status='running',cleanup_token=token,cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=j.id;
 RETURN QUERY SELECT j.id,token,j.tenant_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_tenant_deletion() OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.claim_tenant_deletion() FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.claim_tenant_deletion() TO photo_maintenance;
-- +goose StatementBegin
CREATE FUNCTION photo.checkpoint_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 UPDATE photo.deletion_jobs SET status='queued',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=(cleanup_passes+1)::text WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() AND EXISTS(SELECT FROM photo.tenants t WHERE t.id=tenant_id AND t.status='deleting') RETURNING true
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.checkpoint_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.checkpoint_tenant_deletion(uuid,uuid) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.checkpoint_tenant_deletion(uuid,uuid) TO photo_maintenance;
-- +goose StatementBegin
CREATE FUNCTION photo.finish_tenant_deletion(p_job uuid,p_token uuid) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant' AND status='running' AND cleanup_token=p_token AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.tenants t WHERE t.id=j.tenant_id AND t.status='deleting') OR EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling')) THEN RETURN false;END IF;
 UPDATE photo.memberships SET status='suspended',revision=revision+1 WHERE tenant_id=j.tenant_id AND status='active';
 UPDATE photo.tenants SET status='deleted',revision=revision+1 WHERE id=j.tenant_id AND status='deleting';
 UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=NULL,error=NULL WHERE id=j.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_tenant_deletion(uuid,uuid) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
