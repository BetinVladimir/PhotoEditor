-- +goose Up
-- The legacy finalizer holds the job first. Never wait while acquiring its
-- authority rows: enqueue/purge/decision may already hold those rows before job.
-- A contention exception is caught by the finalizer and retries without writes.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v26_review_media_publication(p_event uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 PERFORM tenant.id FROM photo.media_retry_jobs j
 JOIN photo.tenants tenant ON tenant.id=j.tenant_id
 JOIN photo.reviews review ON review.tenant_id=j.tenant_id AND review.id=j.review_id
 JOIN photo.runs parent ON parent.tenant_id=review.tenant_id AND parent.id=review.run_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=parent.tenant_id AND workflow.run_id=parent.id
 JOIN photo.asset_versions version ON version.tenant_id=j.tenant_id AND version.id=j.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.runs source ON source.tenant_id=j.tenant_id AND source.id=j.origin_run_id
 JOIN photo.artifacts master ON master.tenant_id=version.tenant_id AND master.id=version.master_artifact_id
 WHERE j.event_id=p_event AND j.origin='review'
 FOR SHARE OF tenant,review,parent,workflow,version,asset,source,master NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Own the child run before the finalizer's completion/cancellation update too.
 PERFORM child.id FROM photo.media_retry_jobs j JOIN photo.runs child
 ON child.tenant_id=j.tenant_id AND child.id=j.run_id
 WHERE j.event_id=p_event FOR UPDATE OF child NOWAIT;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.v26_review_media_source_ready(p_event) AND EXISTS(
 SELECT 1 FROM photo.media_retry_jobs j JOIN photo.runs child
 ON child.tenant_id=j.tenant_id AND child.id=j.run_id
 WHERE j.event_id=p_event AND child.status='running' AND child.graph_sealed AND NOT child.cancel_requested);
END $$;
ALTER FUNCTION photo.lock_v26_review_media_publication(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v26_review_media_publication(uuid) FROM PUBLIC;
DO $$
DECLARE body text; needle text;
BEGIN
 body:=pg_get_functiondef('photo.finalize_media_retry_job()'::regprocedure);
 needle:='IF origin_ref=''bulk_repair'' THEN RETURN photo.finalize_media_retry_job_bulk_v1();END IF;';
 IF strpos(body,needle)=0 THEN RAISE EXCEPTION 'REVIEW_MEDIA_FINALIZER_PATCH_MISMATCH';END IF;
 EXECUTE replace(body,needle,needle||$patch$
 IF origin_ref='review' THEN
  BEGIN
   IF NOT photo.lock_v26_review_media_publication(event_ref) THEN
    UPDATE photo.media_retry_jobs SET status='cancelled',error_code='SOURCE_CHANGED',materialize_token=NULL,materialize_lease_until=NULL WHERE event_id=event_ref;
    UPDATE photo.runs SET status='cancelled',cancel_requested=true,revision=revision+1
    WHERE id=run_ref AND status NOT IN('completed','failed','cancelled');
    RETURN true;
   END IF;
  EXCEPTION WHEN lock_not_available THEN
   -- Subtransaction releases authority locks acquired before contention.
   -- The outer job lock remains until this scheduler transaction ends.
   RETURN false;
  END;
 END IF;
$patch$);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
