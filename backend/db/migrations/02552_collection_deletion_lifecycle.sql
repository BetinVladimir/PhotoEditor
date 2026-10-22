-- +goose Up
ALTER TABLE photo.deletion_jobs ADD COLUMN collection_cleanup_retry_at timestamptz;
-- Collection deletion withdraws the live catalogue. Immutable memberships,
-- snapshots, assets and evidence remain reference roots; no object purge occurs.
-- +goose StatementBegin
CREATE FUNCTION photo.claim_collection_deletion() RETURNS TABLE(job_id uuid,cleanup_token uuid,tenant_id uuid,collection_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE; c photo.collections%ROWTYPE; token uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO j FROM photo.deletion_jobs x JOIN photo.collections q ON q.tenant_id=x.tenant_id AND q.id=x.resource_id
 JOIN photo.tenants t ON t.id=x.tenant_id AND t.status='active'
 WHERE x.resource_type='collection' AND (x.collection_cleanup_retry_at IS NULL OR x.collection_cleanup_retry_at<=clock_timestamp()) AND q.state IN('archived','deleting') AND
 (x.status='queued' OR (x.status='running' AND x.cleanup_lease_until<=clock_timestamp()))
 ORDER BY coalesce(x.collection_cleanup_retry_at,x.created_at),x.created_at,x.id LIMIT 1 FOR UPDATE OF x SKIP LOCKED;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT q.* INTO c FROM photo.collections q WHERE q.tenant_id=j.tenant_id AND q.id=j.resource_id FOR UPDATE;
 IF NOT FOUND OR c.state NOT IN('archived','deleting') THEN RETURN;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=j.tenant_id AND t.status='active') THEN RETURN;END IF;
 -- Use the existing cooperative scheduler cancellation protocol. Do not
 -- rewrite task terminals, bypass their fences, or release another attempt.
 UPDATE photo.runs r SET cancel_requested=true,status=CASE WHEN r.status IN('draft','queued','running','waiting_review') THEN 'cancelling' ELSE r.status END,
 revision=r.revision+1 WHERE r.tenant_id=j.tenant_id AND r.status IN('draft','queued','running','waiting_review','cancelling') AND NOT r.cancel_requested AND
 (r.output_collection_id=c.id OR EXISTS(SELECT 1 FROM photo.download_jobs d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.collection_id=c.id AND d.principal_type='guest') OR EXISTS(SELECT 1 FROM photo.upload_sessions u WHERE u.tenant_id=r.tenant_id AND u.ingest_run_id=r.id AND u.collection_id=c.id AND u.principal_type='guest'));
 UPDATE photo.download_jobs d SET cancel_requested=true,status=CASE WHEN d.status IN('queued','preparing','building','uploading') THEN 'cancelling' ELSE d.status END
 WHERE d.tenant_id=j.tenant_id AND d.collection_id=c.id AND d.principal_type='guest' AND d.status NOT IN('ready','failed','cancelled','expired');
 UPDATE photo.bulk_operations b SET cancel_requested=true WHERE b.tenant_id=j.tenant_id AND b.collection_id=c.id AND b.status NOT IN('completed','completed_with_errors','failed','cancelled');
 UPDATE photo.upload_sessions u SET expires_at=least(u.expires_at,clock_timestamp()) WHERE u.tenant_id=j.tenant_id AND u.collection_id=c.id AND u.principal_type='guest' AND u.state IN('initializing','uploading');
 UPDATE photo.deletion_jobs x SET status='running',cleanup_token=token,cleanup_lease_until=clock_timestamp()+interval '30 seconds' WHERE x.id=j.id AND x.tenant_id=j.tenant_id;
 RETURN QUERY SELECT j.id,token,j.tenant_id,c.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_collection_deletion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_collection_deletion() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_collection_deletion() TO photo_maintenance;
-- +goose StatementBegin
CREATE FUNCTION photo.finish_collection_deletion(p_tenant uuid,p_job uuid,p_collection uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.deletion_jobs%ROWTYPE; c photo.collections%ROWTYPE;
BEGIN
 SELECT x.* INTO j FROM photo.deletion_jobs x WHERE x.tenant_id=p_tenant AND x.id=p_job AND x.resource_type='collection' AND x.resource_id=p_collection
 AND x.status='running' AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp() FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT q.* INTO c FROM photo.collections q WHERE q.tenant_id=p_tenant AND q.id=p_collection FOR UPDATE;
 IF NOT FOUND OR c.state NOT IN('archived','deleting') OR NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active') THEN RETURN false;END IF;
 -- The collection lock may have waited beyond the claim's lease. A stale
 -- claimant may not even requeue the job after that wait.
 IF NOT EXISTS(SELECT 1 FROM photo.deletion_jobs x WHERE x.tenant_id=p_tenant AND x.id=p_job
  AND x.resource_type='collection' AND x.resource_id=p_collection AND x.status='running'
  AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp()) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.output_collection_id=p_collection AND r.status IN('draft','queued','running','waiting_review','cancelling'))
 OR EXISTS(SELECT 1 FROM photo.download_jobs d JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id WHERE d.tenant_id=p_tenant AND d.collection_id=p_collection AND d.principal_type='guest' AND r.status IN('draft','queued','running','waiting_review','cancelling'))
 OR EXISTS(SELECT 1 FROM photo.tasks q JOIN photo.runs r ON r.tenant_id=q.tenant_id AND r.id=q.run_id WHERE q.tenant_id=p_tenant AND q.state NOT IN('succeeded','failed','cancelled','skipped') AND (r.output_collection_id=p_collection OR EXISTS(SELECT 1 FROM photo.download_jobs d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.collection_id=p_collection AND d.principal_type='guest') OR EXISTS(SELECT 1 FROM photo.upload_sessions u WHERE u.tenant_id=r.tenant_id AND u.ingest_run_id=r.id AND u.collection_id=p_collection AND u.principal_type='guest')))
 OR EXISTS(SELECT 1 FROM photo.runs r JOIN photo.upload_sessions u ON u.tenant_id=r.tenant_id AND u.ingest_run_id=r.id WHERE u.tenant_id=p_tenant AND u.collection_id=p_collection AND u.principal_type='guest' AND r.status IN('draft','queued','running','waiting_review','cancelling'))
 OR EXISTS(SELECT 1 FROM photo.bulk_operations b WHERE b.tenant_id=p_tenant AND b.collection_id=p_collection AND b.status NOT IN('completed','completed_with_errors','failed','cancelled'))
 OR EXISTS(SELECT 1 FROM photo.upload_sessions u WHERE u.tenant_id=p_tenant AND u.collection_id=p_collection AND u.principal_type='guest' AND u.state IN('initializing','uploading','completing','verifying')) THEN
  UPDATE photo.deletion_jobs SET status='queued',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,collection_cleanup_retry_at=clock_timestamp()+interval '5 seconds' WHERE tenant_id=p_tenant AND id=p_job;RETURN false;
 END IF;
 -- Acquire guest authority row locks before the final lease fence; an awaited
 -- policy/session writer must not let an expired claimant publish a tombstone.
 PERFORM 1 FROM photo.share_links WHERE tenant_id=p_tenant AND collection_id=p_collection ORDER BY id FOR UPDATE;
 PERFORM 1 FROM photo.share_sessions WHERE tenant_id=p_tenant AND collection_id=p_collection ORDER BY id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.deletion_jobs x WHERE x.tenant_id=p_tenant AND x.id=p_job
  AND x.resource_type='collection' AND x.resource_id=p_collection AND x.status='running'
  AND x.cleanup_token=p_token AND x.cleanup_lease_until>clock_timestamp()) THEN RETURN false;END IF;
 UPDATE photo.share_links SET revoked_at=coalesce(revoked_at,clock_timestamp()),policy_version=CASE WHEN revoked_at IS NULL THEN policy_version+1 ELSE policy_version END WHERE tenant_id=p_tenant AND collection_id=p_collection;
 UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp()) WHERE tenant_id=p_tenant AND collection_id=p_collection;
 -- This is a durable logical tombstone, not physical removal of retained
 -- evidence or memberships. Shared original/derived objects are untouched.
 UPDATE photo.collections SET state='deleting',deleted_at=coalesce(deleted_at,clock_timestamp()),revision=revision+1 WHERE tenant_id=p_tenant AND id=p_collection AND deleted_at IS NULL;
 UPDATE photo.deletion_jobs SET status='completed',cleanup_token=NULL,cleanup_lease_until=NULL,cleanup_passes=cleanup_passes+1,cursor=NULL,error=NULL WHERE tenant_id=p_tenant AND id=p_job;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_collection_deletion(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_collection_deletion(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_collection_deletion(uuid,uuid,uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
