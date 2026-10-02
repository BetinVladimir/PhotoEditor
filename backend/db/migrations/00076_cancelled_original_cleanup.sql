-- +goose Up
ALTER TABLE photo.ingest_originals DROP CONSTRAINT original_cleanup_pending_only;
ALTER TABLE photo.ingest_originals ADD CONSTRAINT original_cleanup_reserved_or_orphan
 CHECK(cleanup_state IS NULL OR state IN ('pending','ready'));

-- Pending reservations have no artifacts row. A ready original is eligible only
-- after the cancelled run has a rejection receipt and no catalog adoption.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_original_cleanup()
RETURNS TABLE(artifact_id uuid,cleanup_token uuid,tenant_id uuid,bucket text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.ingest_originals%ROWTYPE;d photo.ingest_originals%ROWTYPE;r photo.runs%ROWTYPE;
 a photo.artifacts%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO hint FROM photo.ingest_originals x JOIN photo.runs rr ON rr.tenant_id=x.tenant_id AND rr.id=x.run_id
 WHERE x.cleanup_after<=clock_timestamp() AND (x.cleanup_lease_until IS NULL OR x.cleanup_lease_until<=clock_timestamp()) AND (
  (x.state='pending' AND (rr.cancel_requested OR rr.status IN ('cancelled','completed','completed_with_errors','failed'))) OR
  (x.state='ready' AND x.cleanup_state IS DISTINCT FROM 'cleaned' AND rr.status='cancelled'
   AND EXISTS(SELECT FROM photo.ingest_rejections f WHERE f.tenant_id=x.tenant_id AND f.run_id=x.run_id AND f.ordinal=x.ordinal)
   AND NOT EXISTS(SELECT FROM photo.ingest_catalog_items c WHERE c.tenant_id=x.tenant_id AND c.run_id=x.run_id AND c.ordinal=x.ordinal))
 ) ORDER BY x.cleanup_after,x.artifact_id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM u.id FROM photo.tenant_usage u WHERE u.tenant_id=hint.tenant_id FOR UPDATE;
 SELECT rr.* INTO r FROM photo.runs rr WHERE rr.tenant_id=hint.tenant_id AND rr.id=hint.run_id FOR UPDATE;
 SELECT * INTO d FROM photo.ingest_originals x WHERE x.artifact_id=hint.artifact_id FOR UPDATE;
 IF NOT FOUND OR d.cleanup_after>clock_timestamp() OR (d.cleanup_lease_until IS NOT NULL AND d.cleanup_lease_until>clock_timestamp()) THEN RETURN;END IF;
 IF d.state='pending' THEN
  IF NOT(r.cancel_requested OR r.status IN ('cancelled','completed','completed_with_errors','failed'))
   OR EXISTS(SELECT FROM photo.artifacts z WHERE z.tenant_id=d.tenant_id AND z.id=d.artifact_id) THEN RETURN;END IF;
 ELSIF d.state='ready' THEN
  IF r.status<>'cancelled' OR d.cleanup_state='cleaned'
   OR NOT EXISTS(SELECT FROM photo.ingest_rejections f WHERE f.tenant_id=d.tenant_id AND f.run_id=d.run_id AND f.ordinal=d.ordinal)
   OR EXISTS(SELECT FROM photo.ingest_catalog_items c WHERE c.tenant_id=d.tenant_id AND c.run_id=d.run_id AND c.ordinal=d.ordinal) THEN RETURN;END IF;
  SELECT * INTO a FROM photo.artifacts z WHERE z.tenant_id=d.tenant_id AND z.id=d.artifact_id FOR UPDATE;
  IF NOT FOUND OR a.state NOT IN ('ready','deleting') OR a.bucket<>d.bucket OR a.object_key<>'originals/tenants/'||d.tenant_id||'/'||d.artifact_id
   OR a.object_version IS DISTINCT FROM d.object_version OR a.sha256<>d.source_sha OR a.bytes<>d.bytes OR a.media_type<>d.media_type
   OR a.output_role<>'original' OR a.created_by_task_id IS NOT NULL THEN RETURN;END IF;
  IF d.cleanup_state IS NULL THEN
   IF a.state<>'ready' THEN RETURN;END IF;
   UPDATE photo.artifacts SET state='deleting' WHERE tenant_id=d.tenant_id AND id=d.artifact_id;
  ELSIF d.cleanup_state='cleaning' AND a.state<>'deleting' THEN RETURN;
  END IF;
 ELSE RETURN;
 END IF;
 UPDATE photo.ingest_originals SET cleanup_state='cleaning',cleanup_token=fence,
  cleanup_lease_until=clock_timestamp()+interval '60 seconds' WHERE ingest_originals.artifact_id=d.artifact_id;
 RETURN QUERY SELECT d.artifact_id,fence,d.tenant_id,d.bucket;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.finish_original_cleanup(p_artifact uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.ingest_originals%ROWTYPE;d photo.ingest_originals%ROWTYPE;a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO hint FROM photo.ingest_originals WHERE artifact_id=p_artifact;
 IF NOT FOUND OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM u.id FROM photo.tenant_usage u WHERE u.tenant_id=hint.tenant_id FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE;
 SELECT * INTO d FROM photo.ingest_originals WHERE artifact_id=p_artifact FOR UPDATE;
 IF d.cleanup_state='cleaned' THEN RETURN d.cleanup_token=p_token;END IF;
 IF d.cleanup_state<>'cleaning' OR d.cleanup_token IS DISTINCT FROM p_token
 OR d.cleanup_lease_until IS NULL OR d.cleanup_lease_until<=clock_timestamp() THEN RETURN false;END IF;
 IF d.state='pending' THEN
  IF EXISTS(SELECT FROM photo.artifacts z WHERE z.tenant_id=d.tenant_id AND z.id=d.artifact_id) THEN RETURN false;END IF;
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes WHERE tenant_id=d.tenant_id AND reserved_bytes>=d.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'ORIGINAL_CLEANUP_ACCOUNTING' USING ERRCODE='23514';END IF;
  UPDATE photo.ingest_originals SET cleanup_state='cleaned',cleanup_token=p_token,
   cleanup_lease_until=clock_timestamp()+interval '24 hours',cleanup_after=clock_timestamp()+interval '24 hours' WHERE artifact_id=p_artifact;
  RETURN true;
 END IF;
 IF d.state<>'ready' THEN RETURN false;END IF;
 SELECT * INTO a FROM photo.artifacts z WHERE z.tenant_id=d.tenant_id AND z.id=d.artifact_id FOR UPDATE;
 IF NOT FOUND OR a.state<>'deleting' OR a.bucket<>d.bucket OR a.object_key<>'originals/tenants/'||d.tenant_id||'/'||d.artifact_id
 OR a.object_version IS DISTINCT FROM d.object_version OR a.sha256<>d.source_sha OR a.bytes<>d.bytes THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET storage_bytes=storage_bytes-d.bytes WHERE tenant_id=d.tenant_id AND storage_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'ORPHAN_ORIGINAL_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.artifacts SET state='deleted' WHERE tenant_id=d.tenant_id AND id=d.artifact_id;
 UPDATE photo.ingest_originals SET cleanup_state='cleaned',cleanup_token=p_token,
  cleanup_lease_until=clock_timestamp()+interval '24 hours',cleanup_after=clock_timestamp()+interval '24 hours' WHERE artifact_id=p_artifact;
 RETURN true;
END $$;
-- +goose StatementEnd

-- Existing owner/grants survive CREATE OR REPLACE, repeat explicitly for audit.
ALTER FUNCTION photo.claim_original_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_original_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_original_cleanup() TO photo_maintenance;
ALTER FUNCTION photo.finish_original_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_original_cleanup(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_original_cleanup(uuid,uuid) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
