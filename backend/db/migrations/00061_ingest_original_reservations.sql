-- +goose Up
CREATE TABLE photo.ingest_originals (
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL,
 source_id uuid NOT NULL,source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 artifact_id uuid NOT NULL UNIQUE,token uuid NOT NULL,bytes bigint NOT NULL CHECK(bytes BETWEEN 1 AND 536870912),
 bucket text NOT NULL,media_type text NOT NULL,state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready')),
 object_version text,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz,
 PRIMARY KEY(tenant_id,run_id,ordinal),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,source_id) REFERENCES photo.artifacts(tenant_id,id),
 CHECK((state='pending' AND object_version IS NULL AND completed_at IS NULL) OR
 (state='ready' AND object_version IS NOT NULL AND object_version NOT IN ('','null') AND completed_at IS NOT NULL))
);
ALTER TABLE photo.ingest_originals ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_originals FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_originals TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.ingest_originals TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_ingest_original(p_tenant uuid,p_run uuid,p_ordinal bigint,p_source uuid,p_sha text,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,object_version text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;s photo.artifacts%ROWTYPE;d photo.ingest_originals%ROWTYPE;
 capacity numeric;used numeric;aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();
BEGIN
 IF p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 9007199254740991 OR p_source IS NULL
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RAISE EXCEPTION 'INVALID_ORIGINAL' USING ERRCODE='22023';END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.phase IN ('decode','catalog')) THEN
 RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text;RETURN;END IF;
 SELECT a.* INTO s FROM photo.snapshot_items i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id AND i.ordinal=p_ordinal AND i.artifact_id=p_source AND i.sha256=p_sha
 AND a.state='ready' AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null') AND a.bytes BETWEEN 1 AND 536870912
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF NOT FOUND OR EXISTS(SELECT FROM (VALUES('asset.verify'),('asset.inspect'),('asset.decode')) op(name)
 WHERE NOT EXISTS(SELECT FROM photo.ingest_node_plans n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
 WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.ordinal=p_ordinal AND n.operation=op.name AND n.source_artifact_id=p_source AND n.source_sha=p_sha
 AND t.operation=op.name AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL)) THEN
 RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,NULL::text;RETURN;END IF;
 SELECT * INTO d FROM photo.ingest_originals x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal FOR UPDATE;
 IF FOUND THEN
  IF d.source_id<>p_source OR d.source_sha<>p_sha OR d.bytes<>s.bytes OR d.media_type<>s.media_type OR d.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::text;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.source_sha AND a.object_version=d.object_version) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,d.object_version;RETURN;
  END IF;
  UPDATE photo.ingest_originals SET token=fence WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,NULL::text;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+s.bytes>capacity OR used+s.bytes>9007199254740991 THEN
 RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text;RETURN;END IF;
 INSERT INTO photo.ingest_originals(tenant_id,run_id,ordinal,source_id,source_sha,artifact_id,token,bytes,bucket,media_type)
 VALUES(p_tenant,p_run,p_ordinal,p_source,p_sha,aid,fence,s.bytes,p_bucket,s.media_type);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+s.bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,NULL::text;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_ingest_original(uuid,uuid,bigint,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_ingest_original(uuid,uuid,bigint,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_ingest_original(uuid,uuid,bigint,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_ingest_original(p_tenant uuid,p_run uuid,p_ordinal bigint,p_artifact uuid,p_token uuid,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.ingest_originals%ROWTYPE;r photo.runs%ROWTYPE;target text;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR NOT r.graph_sealed OR r.status NOT IN ('queued','running')
 OR NOT EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.phase IN ('decode','catalog')) THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.ingest_originals x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.sha256=d.source_sha AND a.object_version=p_version);END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 target:='originals/tenants/'||p_tenant||'/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,d.tenant_id,d.bucket,target,p_version,d.source_sha,d.bytes,d.media_type,'ready',clock_timestamp(),'original');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes
 WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'ORIGINAL_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.ingest_originals SET state='ready',object_version=p_version,completed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_ingest_original(uuid,uuid,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_ingest_original(uuid,uuid,bigint,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_ingest_original(uuid,uuid,bigint,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
