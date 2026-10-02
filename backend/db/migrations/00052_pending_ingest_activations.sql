-- +goose Up
-- Candidate discovery only; activate_bound_task rechecks under authoritative locks.
-- +goose StatementBegin
CREATE FUNCTION photo.pending_ingest_activations(p_limit integer)
RETURNS TABLE(task_id uuid,params_id uuid,input_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'INVALID_LIMIT' USING ERRCODE='22023';END IF;
 RETURN QUERY SELECT t.id,t.params_artifact_id,t.input_digest
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.ingest_progress p ON p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.phase='inspection'
 JOIN photo.ingest_node_plans n ON n.tenant_id=t.tenant_id AND n.run_id=t.run_id AND n.id=t.id AND n.operation=t.operation
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id AND a.state='ready' AND a.sha256=n.params_sha
 JOIN photo.artifacts source ON source.tenant_id=n.tenant_id AND source.id=n.source_artifact_id AND source.state='ready' AND source.sha256=n.source_sha
 WHERE r.kind='ingest' AND r.graph_sealed AND NOT r.cancel_requested AND r.status IN ('queued','running')
 AND t.state='blocked' AND NOT t.cancel_requested AND t.terminal_reason IS NULL AND t.operation IN ('asset.verify','asset.inspect')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.state<>'closed')
 AND NOT EXISTS(SELECT FROM photo.task_edges e JOIN photo.tasks predecessor ON predecessor.tenant_id=e.tenant_id AND predecessor.id=e.prerequisite_id
  WHERE e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.dependent_id=t.id
  AND (e.condition<>'success' OR predecessor.state<>'succeeded' OR predecessor.canonical_result_id IS NULL))
 ORDER BY r.created_at,r.id,n.ordinal,n.operation LIMIT p_limit;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_ingest_activations(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_ingest_activations(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pending_ingest_activations(integer) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
