-- +goose Up
ALTER TABLE photo.ingest_rejections DROP CONSTRAINT ingest_rejections_failure_stage_check;
ALTER TABLE photo.ingest_rejections ADD CONSTRAINT ingest_rejections_failure_stage_check
 CHECK(failure_stage IN ('asset.verify','asset.inspect','asset.decode','run.cancel'));

-- Settle one upload only after every task in the cancelled run is terminal.
-- Staging still exists, so reserved quota becomes charged storage and the
-- ordinary staging cleanup queue remains responsible for physical deletion.
-- +goose StatementBegin
CREATE FUNCTION photo.cancel_ingest_upload() RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.ingest_upload_bindings%ROWTYPE;b photo.ingest_upload_bindings%ROWTYPE;
 r photo.runs%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 link photo.share_links%ROWTYPE;actual bigint;reserved bigint;
BEGIN
 SELECT x.* INTO hint FROM photo.ingest_upload_bindings x
 JOIN photo.runs rr ON rr.tenant_id=x.tenant_id AND rr.id=x.run_id
 WHERE rr.kind='ingest' AND rr.cancel_requested AND rr.status='cancelling'
 AND NOT EXISTS(SELECT FROM photo.ingest_catalog_items c WHERE c.tenant_id=x.tenant_id AND c.run_id=x.run_id AND c.ordinal=x.ordinal AND c.settled_at IS NOT NULL)
 AND NOT EXISTS(SELECT FROM photo.ingest_rejections f WHERE f.tenant_id=x.tenant_id AND f.run_id=x.run_id AND f.ordinal=x.ordinal)
 AND NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=rr.tenant_id AND t.run_id=rr.id AND t.state NOT IN ('succeeded','failed','cancelled','skipped'))
 ORDER BY rr.created_at,x.ordinal LIMIT 1;
 IF NOT FOUND THEN RETURN 0;END IF;
 SELECT reserved_bytes INTO reserved FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RETURN 0;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=hint.tenant_id AND id=hint.upload_id;
 IF NOT FOUND THEN RETURN 0;END IF;
 IF u.share_id IS NOT NULL THEN
  SELECT * INTO link FROM photo.share_links WHERE tenant_id=hint.tenant_id AND id=u.share_id FOR UPDATE;
  IF NOT FOUND THEN RETURN 0;END IF;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR NOT r.cancel_requested OR r.status<>'cancelling'
 OR EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN ('succeeded','failed','cancelled','skipped')) THEN RETURN 0;END IF;
 SELECT * INTO b FROM photo.ingest_upload_bindings x WHERE x.tenant_id=hint.tenant_id AND x.run_id=hint.run_id AND x.ordinal=hint.ordinal;
 IF NOT FOUND OR b.upload_id<>hint.upload_id OR b.source_id<>hint.source_id OR b.source_sha<>hint.source_sha THEN RETURN 0;END IF;
 IF EXISTS(SELECT FROM photo.ingest_catalog_items c WHERE c.tenant_id=b.tenant_id AND c.run_id=b.run_id AND c.ordinal=b.ordinal AND c.settled_at IS NOT NULL)
 OR EXISTS(SELECT FROM photo.ingest_rejections f WHERE f.tenant_id=b.tenant_id AND f.run_id=b.run_id AND f.ordinal=b.ordinal) THEN RETURN 0;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=b.tenant_id AND id=b.upload_id FOR UPDATE;
 IF NOT FOUND OR u.state<>'verifying' OR u.verified_version_id IS NOT NULL OR u.staging_artifact_id IS DISTINCT FROM b.source_id
 OR u.project_id<>b.project_id OR u.collection_id IS DISTINCT FROM b.collection_id OR u.principal_type<>b.principal_type OR u.principal_id<>b.principal_id
 OR u.share_id IS DISTINCT FROM link.id OR u.revision>=9007199254740991 THEN RETURN 0;END IF;
 SELECT bytes INTO actual FROM photo.artifacts WHERE tenant_id=b.tenant_id AND id=b.source_id AND sha256=b.source_sha AND state='ready';
 IF NOT FOUND OR actual IS DISTINCT FROM u.actual_size OR actual NOT BETWEEN 1 AND 536870912 THEN RETURN 0;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=b.tenant_id AND id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.principal_type<>u.principal_type OR q.principal_id<>u.principal_id
 OR q.share_id IS DISTINCT FROM u.share_id OR q.files<>1 OR q.bytes<>u.expected_size OR q.bytes<actual OR reserved<q.bytes THEN RETURN 0;END IF;
 IF link.id IS NOT NULL AND (link.reserved_bytes<q.bytes OR link.reserved_files<1) THEN RETURN 0;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes,storage_bytes=storage_bytes+actual
 WHERE tenant_id=b.tenant_id AND reserved_bytes>=q.bytes AND storage_bytes<=9007199254740991-actual;
 IF NOT FOUND THEN RAISE EXCEPTION 'CANCELLED_UPLOAD_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 IF link.id IS NOT NULL THEN
  UPDATE photo.share_links SET reserved_bytes=reserved_bytes-q.bytes,reserved_files=reserved_files-1
  WHERE tenant_id=b.tenant_id AND id=link.id;
 END IF;
 UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=b.tenant_id AND id=q.id;
 UPDATE photo.upload_sessions SET state='rejected',revision=revision+1 WHERE tenant_id=b.tenant_id AND id=u.id;
 INSERT INTO photo.ingest_rejections(tenant_id,run_id,ordinal,failure_stage,failure_reason,staging_bytes)
 VALUES(b.tenant_id,b.run_id,b.ordinal,'run.cancel','cancelled',actual);
 RETURN 1;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.cancel_ingest_upload() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.cancel_ingest_upload() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.cancel_ingest_upload() TO photo_scheduler,photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
