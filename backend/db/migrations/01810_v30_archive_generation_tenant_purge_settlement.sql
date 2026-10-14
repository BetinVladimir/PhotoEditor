-- +goose Up
-- Tenant deletion purges the complete tenant prefix while ordinary archive
-- cleanup requires an active tenant. After the storage adapter reports an
-- empty prefix, this maintenance-only transition records that proof for
-- generations with a durable successful Create receipt. Unknown Create and
-- open legacy writers remain fenced.
-- +goose StatementBegin
CREATE FUNCTION photo.settle_v30_archive_generations_after_tenant_purge(p_job uuid,p_token uuid,p_tenant uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE;
BEGIN
 SELECT * INTO j FROM photo.deletion_jobs WHERE id=p_job AND resource_type='tenant'
  AND tenant_id=p_tenant AND status='running' AND cleanup_token=p_token
  AND cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND THEN RETURN 'blocked';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='deleting' FOR UPDATE;
 IF NOT FOUND THEN RETURN 'blocked';END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.state='open')
  OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=p_tenant
   AND (g.state IN('creating','create_failed')
    OR (g.state='aborted' AND g.purged_at IS NULL)
    OR (g.state IN('created','bound','aborting') AND
     (g.multipart_id IS NULL OR g.terminal_evidence<>'s3-create-receipt'
      OR NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=g.tenant_id
       AND w.upload_id=g.upload_id AND w.token=g.writer_token AND w.kind='begin'
       AND w.state='closed' AND w.receipt=g.multipart_id))))) THEN RETURN 'blocked';END IF;
 WITH batch AS (SELECT tenant_id,upload_id,generation_id FROM photo.v30_archive_begin_generations
  WHERE tenant_id=p_tenant AND state IN('created','bound','aborting')
  ORDER BY upload_id,generation_id LIMIT 256 FOR UPDATE)
 UPDATE photo.v30_archive_begin_generations g SET state='aborted',purged_at=clock_timestamp(),
  cleanup_token=NULL,cleanup_lease_until=NULL,revision=g.revision+1
 FROM batch b WHERE g.tenant_id=b.tenant_id AND g.upload_id=b.upload_id AND g.generation_id=b.generation_id;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=p_tenant
  AND g.state IN('created','bound','aborting')) THEN RETURN 'more';END IF;
 RETURN 'complete';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.settle_v30_archive_generations_after_tenant_purge(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.settle_v30_archive_generations_after_tenant_purge(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.settle_v30_archive_generations_after_tenant_purge(uuid,uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
