-- +goose Up
-- Lease-bound source location is never exposed through the public API.
-- +goose StatementBegin
CREATE FUNCTION photo.ingest_original_source(p_tenant uuid,p_run uuid,p_ordinal bigint,p_artifact uuid,p_token uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.ingest_originals d JOIN photo.runs r ON r.tenant_id=d.tenant_id AND r.id=d.run_id
 JOIN photo.ingest_progress p ON p.tenant_id=r.tenant_id AND p.run_id=r.id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.source_id AND a.sha256=d.source_sha AND a.bytes=d.bytes AND a.media_type=d.media_type
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.ordinal=p_ordinal AND d.artifact_id=p_artifact AND d.token=p_token AND d.state='pending'
 AND r.kind='ingest' AND r.status IN ('queued','running') AND NOT r.cancel_requested AND r.graph_sealed AND p.phase IN ('decode','catalog')
 AND a.state='ready' AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>statement_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.ingest_original_source(uuid,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ingest_original_source(uuid,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.ingest_original_source(uuid,uuid,bigint,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
