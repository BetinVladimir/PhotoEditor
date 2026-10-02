-- +goose Up
-- A single statement returns phase and every source binding from one MVCC snapshot.
-- Missing/expired sources cause incomplete rows, rejected by the Go loader.
-- +goose StatementBegin
CREATE FUNCTION photo.ingest_run_context(p_tenant uuid,p_run uuid)
RETURNS TABLE(phase text,graph_sealed boolean,item_count bigint,ordinal bigint,source_id uuid,source_sha text,source_bytes bigint,payload jsonb)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p.phase,'planning'),r.graph_sealed,s.item_count,i.ordinal,a.id,a.sha256,a.bytes,i.payload
 FROM photo.runs r
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.snapshot_items i ON i.tenant_id=s.tenant_id AND i.snapshot_id=s.id
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 LEFT JOIN photo.ingest_progress p ON p.tenant_id=r.tenant_id AND p.run_id=r.id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='ingest'
 AND r.status IN ('queued','running') AND NOT r.cancel_requested
 AND s.item_count BETWEEN 1 AND 10000
 AND (s.expires_at IS NULL OR s.expires_at>statement_timestamp())
 AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>statement_timestamp())
 AND (p.run_id IS NULL OR p.item_count=s.item_count)
 ORDER BY i.ordinal LIMIT 10001;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.ingest_run_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ingest_run_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.ingest_run_context(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
