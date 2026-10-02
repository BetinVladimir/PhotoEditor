-- +goose Up
-- Immutable catalog destination captured from an explicit upload session.
CREATE TABLE photo.ingest_upload_bindings (
 tenant_id uuid NOT NULL,run_id uuid NOT NULL,ordinal bigint NOT NULL CHECK(ordinal BETWEEN 0 AND 9007199254740991),
 upload_id uuid NOT NULL,source_id uuid NOT NULL,source_sha text NOT NULL CHECK(source_sha ~ '^[0-9a-f]{64}$'),
 project_id uuid NOT NULL,collection_id uuid,principal_type text NOT NULL,principal_id uuid NOT NULL,
 asset_id uuid NOT NULL DEFAULT gen_random_uuid(),version_id uuid NOT NULL DEFAULT gen_random_uuid(),item_id uuid NOT NULL DEFAULT gen_random_uuid(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,run_id,ordinal),UNIQUE(tenant_id,upload_id),UNIQUE(asset_id),UNIQUE(version_id),UNIQUE(item_id),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,upload_id) REFERENCES photo.upload_sessions(tenant_id,id),
 FOREIGN KEY(tenant_id,source_id) REFERENCES photo.artifacts(tenant_id,id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 FOREIGN KEY(tenant_id,collection_id) REFERENCES photo.collections(tenant_id,id)
);
ALTER TABLE photo.ingest_upload_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.ingest_upload_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.ingest_upload_bindings TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.ingest_upload_bindings TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.bind_ingest_upload(p_tenant uuid,p_run uuid,p_ordinal bigint,p_upload uuid)
RETURNS TABLE(asset_id uuid,version_id uuid,item_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;u photo.upload_sessions%ROWTYPE;s photo.snapshot_items%ROWTYPE;b photo.ingest_upload_bindings%ROWTYPE;
BEGIN
 IF p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 9007199254740991 OR p_upload IS NULL THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR r.status NOT IN ('draft','queued','running') THEN RETURN;END IF;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO b FROM photo.ingest_upload_bindings x WHERE x.tenant_id=p_tenant AND x.run_id=p_run AND x.ordinal=p_ordinal;
 IF FOUND THEN
  IF b.upload_id=p_upload THEN RETURN QUERY SELECT b.asset_id,b.version_id,b.item_id;END IF;RETURN;
 END IF;
 IF r.graph_sealed OR EXISTS(SELECT FROM photo.ingest_progress p WHERE p.tenant_id=p_tenant AND p.run_id=p_run)
 OR u.state<>'verifying' OR u.staging_artifact_id IS NULL OR u.verified_version_id IS NOT NULL OR u.actual_size IS NULL
 OR EXISTS(SELECT FROM photo.ingest_upload_bindings x WHERE x.tenant_id=p_tenant AND x.upload_id=p_upload) THEN RETURN;END IF;
 SELECT i.* INTO s FROM photo.snapshot_items i JOIN photo.snapshots snap ON snap.tenant_id=i.tenant_id AND snap.id=i.snapshot_id
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id AND i.ordinal=p_ordinal AND i.artifact_id=u.staging_artifact_id
 AND snap.principal_type=u.principal_type AND snap.principal_id=u.principal_id AND a.state='ready' AND a.bytes=u.actual_size
 AND a.object_key=u.object_key AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND (snap.expires_at IS NULL OR snap.expires_at>clock_timestamp());
 IF NOT FOUND OR NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=u.project_id AND p.archived_at IS NULL)
 OR (u.collection_id IS NOT NULL AND NOT EXISTS(SELECT FROM photo.collections c WHERE c.tenant_id=p_tenant AND c.id=u.collection_id AND c.project_id=u.project_id AND c.deleted_at IS NULL)) THEN RETURN;END IF;
 INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id)
 VALUES(p_tenant,p_run,p_ordinal,p_upload,s.artifact_id,s.sha256,u.project_id,u.collection_id,u.principal_type,u.principal_id)
 RETURNING * INTO b;
 RETURN QUERY SELECT b.asset_id,b.version_id,b.item_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.bind_ingest_upload(uuid,uuid,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_ingest_upload(uuid,uuid,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.bind_ingest_upload(uuid,uuid,bigint,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
