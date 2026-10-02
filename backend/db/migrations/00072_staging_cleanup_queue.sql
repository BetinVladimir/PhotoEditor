-- +goose Up
-- artifact_id is historical cleanup identity, deliberately not a live-use FK.
CREATE TABLE photo.staging_cleanup (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL,
 artifact_id uuid NOT NULL,staging_bytes bigint NOT NULL CHECK(staging_bytes BETWEEN 1 AND 536870912),
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','cleaning','cleaned')),
 next_at timestamptz NOT NULL,token uuid,lease_until timestamptz,completed_at timestamptz,
 bucket text,object_key text,object_version text,sha256 text,media_type text,
 UNIQUE(tenant_id,run_id,ordinal),FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 CHECK((token IS NULL)=(lease_until IS NULL)),
 CHECK(state='queued' OR (bucket IS NOT NULL AND object_key IS NOT NULL AND object_version IS NOT NULL AND object_version NOT IN ('','null') AND sha256 IS NOT NULL AND media_type IS NOT NULL))
);
ALTER TABLE photo.staging_cleanup ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.staging_cleanup FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.staging_cleanup TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.staging_cleanup TO photo_fn_owner;
CREATE INDEX staging_cleanup_due ON photo.staging_cleanup(next_at,id) WHERE state<>'cleaned';
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_staging_cleanup() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.settled_at IS NOT NULL THEN
  INSERT INTO photo.staging_cleanup(tenant_id,run_id,ordinal,artifact_id,staging_bytes,next_at)
  SELECT NEW.tenant_id,NEW.run_id,NEW.ordinal,b.source_id,NEW.staging_bytes,NEW.settled_at+interval '7 days'
  FROM photo.ingest_upload_bindings b WHERE b.tenant_id=NEW.tenant_id AND b.run_id=NEW.run_id AND b.ordinal=NEW.ordinal
  ON CONFLICT(tenant_id,run_id,ordinal) DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_staging_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_staging_cleanup() FROM PUBLIC;
CREATE TRIGGER enqueue_staging_cleanup AFTER INSERT OR UPDATE OF settled_at ON photo.ingest_catalog_items FOR EACH ROW EXECUTE FUNCTION photo.enqueue_staging_cleanup();
CREATE TRIGGER enqueue_staging_cleanup AFTER INSERT ON photo.ingest_rejections FOR EACH ROW EXECUTE FUNCTION photo.enqueue_staging_cleanup();
INSERT INTO photo.staging_cleanup(tenant_id,run_id,ordinal,artifact_id,staging_bytes,next_at)
SELECT x.tenant_id,x.run_id,x.ordinal,b.source_id,x.staging_bytes,x.settled_at+interval '7 days'
FROM (SELECT tenant_id,run_id,ordinal,staging_bytes,settled_at FROM photo.ingest_catalog_items WHERE settled_at IS NOT NULL
 UNION ALL SELECT tenant_id,run_id,ordinal,staging_bytes,settled_at FROM photo.ingest_rejections) x
JOIN photo.ingest_upload_bindings b ON b.tenant_id=x.tenant_id AND b.run_id=x.run_id AND b.ordinal=x.ordinal
ON CONFLICT(tenant_id,run_id,ordinal) DO NOTHING;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_staging_cleanup()
RETURNS TABLE(cleanup_id uuid,cleanup_token uuid,tenant_id uuid,artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.staging_cleanup%ROWTYPE;d photo.staging_cleanup%ROWTYPE;r photo.runs%ROWTYPE;b photo.ingest_upload_bindings%ROWTYPE;
 a photo.artifacts%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'CLEANUP_REQUIRES_READ_COMMITTED';END IF;
 SELECT x.* INTO hint FROM photo.staging_cleanup x WHERE x.state<>'cleaned' AND x.next_at<=clock_timestamp()
 AND (x.lease_until IS NULL OR x.lease_until<=clock_timestamp()) ORDER BY x.next_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM u.id FROM photo.tenant_usage u WHERE u.tenant_id=hint.tenant_id FOR UPDATE;
 SELECT rr.* INTO r FROM photo.runs rr WHERE rr.tenant_id=hint.tenant_id AND rr.id=hint.run_id FOR UPDATE;
 SELECT x.* INTO d FROM photo.staging_cleanup x WHERE x.id=hint.id FOR UPDATE;
 IF d.state='cleaned' OR d.next_at>clock_timestamp() OR (d.lease_until IS NOT NULL AND d.lease_until>clock_timestamp()) THEN RETURN;END IF;
 -- Defer before checks so a referenced/unfinished head does not starve the queue.
 UPDATE photo.staging_cleanup SET next_at=clock_timestamp()+interval '1 hour' WHERE id=d.id;
 IF r.id IS NULL OR r.kind<>'ingest' OR r.status NOT IN ('completed','completed_with_errors','failed','cancelled')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.phase='complete') THEN RETURN;END IF;
 SELECT x.* INTO b FROM photo.ingest_upload_bindings x WHERE x.tenant_id=d.tenant_id AND x.run_id=d.run_id AND x.ordinal=d.ordinal;
 IF NOT FOUND OR b.source_id<>d.artifact_id THEN RETURN;END IF;
 IF NOT EXISTS(SELECT FROM photo.upload_sessions u WHERE u.tenant_id=b.tenant_id AND u.id=b.upload_id AND u.staging_artifact_id=d.artifact_id AND u.state IN ('verified','rejected')) THEN RETURN;END IF;
 SELECT ar.* INTO a FROM photo.artifacts ar WHERE ar.tenant_id=d.tenant_id AND ar.id=d.artifact_id FOR UPDATE;
 IF NOT FOUND OR a.state NOT IN ('ready','deleting') OR a.bytes<>d.staging_bytes OR a.object_version IS NULL OR a.object_version IN ('','null') THEN RETURN;END IF;
 IF photo.staging_has_references(d.tenant_id,d.artifact_id,d.run_id,b.upload_id,r.input_snapshot_id) THEN RETURN;END IF;
 IF d.state='queued' THEN
  IF a.state<>'ready' THEN RETURN;END IF;
  UPDATE photo.artifacts SET state='deleting' WHERE id=a.id AND artifacts.tenant_id=d.tenant_id;
  UPDATE photo.staging_cleanup SET state='cleaning',bucket=a.bucket,object_key=a.object_key,object_version=a.object_version,sha256=a.sha256,media_type=a.media_type WHERE id=d.id;
 ELSE
  IF a.state<>'deleting' OR a.bucket IS DISTINCT FROM d.bucket OR a.object_key IS DISTINCT FROM d.object_key OR a.object_version IS DISTINCT FROM d.object_version OR a.sha256 IS DISTINCT FROM d.sha256 OR a.media_type IS DISTINCT FROM d.media_type THEN RETURN;END IF;
 END IF;
 UPDATE photo.staging_cleanup SET token=fence,lease_until=clock_timestamp()+interval '60 seconds',next_at=clock_timestamp()+interval '60 seconds' WHERE id=d.id;
 RETURN QUERY SELECT d.id,fence,d.tenant_id,d.artifact_id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_staging_cleanup() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_staging_cleanup() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_staging_cleanup() TO photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.finish_staging_cleanup(p_id uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.staging_cleanup%ROWTYPE;d photo.staging_cleanup%ROWTYPE;a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO hint FROM photo.staging_cleanup WHERE id=p_id;
 IF NOT FOUND OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=hint.tenant_id AND id=hint.run_id FOR UPDATE;
 SELECT * INTO d FROM photo.staging_cleanup WHERE id=p_id FOR UPDATE;
 IF d.state='cleaned' THEN RETURN d.token=p_token;END IF;
 IF d.state<>'cleaning' OR d.token IS DISTINCT FROM p_token OR d.lease_until IS NULL OR d.lease_until<=clock_timestamp() THEN RETURN false;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=d.tenant_id AND id=d.artifact_id FOR UPDATE;
 IF NOT FOUND OR a.state<>'deleting' OR a.bytes<>d.staging_bytes OR a.bucket IS DISTINCT FROM d.bucket OR a.object_key IS DISTINCT FROM d.object_key
 OR a.object_version IS DISTINCT FROM d.object_version OR a.sha256 IS DISTINCT FROM d.sha256 OR a.media_type IS DISTINCT FROM d.media_type THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET storage_bytes=storage_bytes-d.staging_bytes WHERE tenant_id=d.tenant_id AND storage_bytes>=d.staging_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'STAGING_CLEANUP_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.artifacts SET state='deleted' WHERE tenant_id=d.tenant_id AND id=d.artifact_id;
 -- Keep the final token for replay; the expired lease is no longer discoverable.
 UPDATE photo.staging_cleanup SET state='cleaned',completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_staging_cleanup(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_staging_cleanup(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finish_staging_cleanup(uuid,uuid) TO photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
