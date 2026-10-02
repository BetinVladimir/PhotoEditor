-- +goose Up
CREATE TABLE photo.ingest_rejections (
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL,
 failure_stage text NOT NULL CHECK(failure_stage IN ('asset.verify','asset.inspect','asset.decode')),
 failure_reason text NOT NULL CHECK(failure_reason IN ('failed','cancelled')),
 staging_bytes bigint NOT NULL CHECK(staging_bytes BETWEEN 1 AND 536870912),settled_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,run_id,ordinal) REFERENCES photo.ingest_upload_bindings(tenant_id,run_id,ordinal)
);
ALTER TABLE photo.ingest_rejections ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_rejections FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_rejections TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.ingest_rejections TO photo_fn_owner;
-- +goose StatementBegin
CREATE FUNCTION photo.reject_ingest_upload(p_tenant uuid,p_run uuid,p_ordinal bigint,p_source uuid,p_sha text,p_stage text,p_reason text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.ingest_upload_bindings%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 r photo.runs%ROWTYPE;link photo.share_links%ROWTYPE;item photo.ingest_decode_items%ROWTYPE;old photo.ingest_rejections%ROWTYPE;
 actual bigint;reserved bigint;state_now text;
BEGIN
 IF p_stage IS NULL OR p_stage NOT IN ('asset.verify','asset.inspect','asset.decode') OR p_reason IS NULL OR p_reason NOT IN ('failed','cancelled') THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.ingest_upload_bindings x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF NOT FOUND OR b.source_id IS DISTINCT FROM p_source OR b.source_sha IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 SELECT reserved_bytes INTO reserved FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant AND id=b.upload_id;
 IF NOT FOUND THEN RETURN false;END IF;
 IF u.share_id IS NOT NULL THEN
  SELECT * INTO link FROM photo.share_links WHERE tenant_id=p_tenant AND id=u.share_id FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.phase IN ('decode','catalog')) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.ingest_rejections x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF FOUND THEN RETURN old.failure_stage=p_stage AND old.failure_reason=p_reason AND EXISTS(SELECT FROM photo.upload_sessions x WHERE x.tenant_id=p_tenant AND x.id=b.upload_id AND x.state='rejected');END IF;
 IF EXISTS(SELECT FROM photo.ingest_catalog_items x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal)
 OR EXISTS(SELECT FROM photo.ingest_originals x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal) THEN RETURN false;END IF;
 SELECT * INTO item FROM photo.ingest_decode_items x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_stage IN ('asset.verify','asset.inspect') THEN
  IF item.outcome<>'failed' OR item.failure_stage IS DISTINCT FROM p_stage OR item.failure_reason IS DISTINCT FROM p_reason THEN RETURN false;END IF;
 ELSE
  IF item.outcome<>'decode' THEN RETURN false;END IF;
 END IF;
 SELECT t.state INTO state_now FROM photo.ingest_node_plans n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
 WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.ordinal=p_ordinal AND n.operation=p_stage AND n.source_artifact_id=b.source_id AND n.source_sha=b.source_sha AND t.operation=p_stage;
 IF NOT FOUND OR state_now IS DISTINCT FROM p_reason THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant AND id=b.upload_id FOR UPDATE;
 IF NOT FOUND OR u.state<>'verifying' OR u.verified_version_id IS NOT NULL OR u.staging_artifact_id IS DISTINCT FROM b.source_id
 OR u.project_id<>b.project_id OR u.collection_id IS DISTINCT FROM b.collection_id OR u.principal_type<>b.principal_type OR u.principal_id<>b.principal_id
 OR u.share_id IS DISTINCT FROM link.id OR u.revision>=9007199254740991 THEN RETURN false;END IF;
 SELECT bytes INTO actual FROM photo.artifacts WHERE tenant_id=p_tenant AND id=b.source_id AND sha256=b.source_sha AND state='ready';
 IF NOT FOUND OR actual IS DISTINCT FROM u.actual_size OR actual NOT BETWEEN 1 AND 536870912 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.principal_type<>u.principal_type OR q.principal_id<>u.principal_id
 OR q.share_id IS DISTINCT FROM u.share_id OR q.files<>1 OR q.bytes<>u.expected_size OR q.bytes<actual OR reserved<q.bytes THEN RETURN false;END IF;
 IF link.id IS NOT NULL AND (link.reserved_bytes<q.bytes OR link.reserved_files<1) THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes,storage_bytes=storage_bytes+actual
 WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes AND storage_bytes<=9007199254740991-actual;
 IF NOT FOUND THEN RAISE EXCEPTION 'REJECTED_UPLOAD_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 IF link.id IS NOT NULL THEN
  UPDATE photo.share_links SET reserved_bytes=reserved_bytes-q.bytes,reserved_files=reserved_files-1 WHERE tenant_id=p_tenant AND id=link.id;
 END IF;
 UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=p_tenant AND id=q.id;
 UPDATE photo.upload_sessions SET state='rejected',revision=revision+1 WHERE tenant_id=p_tenant AND id=u.id;
 INSERT INTO photo.ingest_rejections(tenant_id,run_id,ordinal,failure_stage,failure_reason,staging_bytes)
 VALUES(p_tenant,p_run,p_ordinal,p_stage,p_reason,actual);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reject_ingest_upload(uuid,uuid,bigint,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reject_ingest_upload(uuid,uuid,bigint,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reject_ingest_upload(uuid,uuid,bigint,uuid,text,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
