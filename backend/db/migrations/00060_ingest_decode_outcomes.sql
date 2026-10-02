-- +goose Up
-- Canonical decode outputs for catalog validation; this function never changes
-- run phase or marks a file imported. The caller must reject incomplete batches.
-- +goose StatementBegin
CREATE FUNCTION photo.ingest_decode_outcomes(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal bigint,source_id uuid,source_sha text,outcome text,failure_stage text,failure_reason text,
 decode_task_id uuid,decode_state text,metadata jsonb,outputs jsonb)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT item.ordinal,s.artifact_id,s.sha256,item.outcome,item.failure_stage,item.failure_reason,
 t.id,t.state,
 CASE WHEN m.id IS NOT NULL THEN jsonb_build_object('artifact_id',m.id,'sha256',m.sha256,'bucket',m.bucket,'key',m.object_key,'version',m.object_version,'bytes',m.bytes,'media_type',m.media_type) ELSE NULL END,
 CASE WHEN t.state='succeeded' THEN o.docs ELSE NULL END
 FROM photo.runs r
 JOIN photo.ingest_progress p ON p.tenant_id=r.tenant_id AND p.run_id=r.id
 JOIN photo.ingest_decode_items item ON item.tenant_id=r.tenant_id AND item.run_id=r.id
 JOIN photo.snapshot_items s ON s.tenant_id=r.tenant_id AND s.snapshot_id=r.input_snapshot_id AND s.ordinal=item.ordinal
 JOIN photo.artifacts source ON source.tenant_id=s.tenant_id AND source.id=s.artifact_id AND source.sha256=s.sha256
 LEFT JOIN photo.ingest_node_plans plan ON plan.tenant_id=item.tenant_id AND plan.run_id=item.run_id AND plan.ordinal=item.ordinal AND plan.operation='asset.decode'
 LEFT JOIN photo.tasks t ON t.tenant_id=plan.tenant_id AND t.run_id=plan.run_id AND t.id=plan.id AND t.operation='asset.decode'
 LEFT JOIN photo.artifacts m ON m.tenant_id=item.tenant_id AND m.id=item.metadata_id AND m.sha256=item.metadata_sha AND m.state='ready'
  AND (m.expires_at IS NULL OR m.expires_at>statement_timestamp())
 LEFT JOIN LATERAL (
  SELECT jsonb_agg(jsonb_build_object('role',a.output_role,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY a.output_role,a.id) docs
  FROM photo.task_results z JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.created_by_task_id=z.task_id
  WHERE z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id AND a.state='ready'
  AND (a.expires_at IS NULL OR a.expires_at>statement_timestamp())
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 ) o ON true
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='ingest' AND r.status IN ('queued','running') AND NOT r.cancel_requested
 AND r.graph_sealed AND p.phase IN ('decode','catalog') AND source.state='ready'
 AND (source.expires_at IS NULL OR source.expires_at>statement_timestamp())
 ORDER BY item.ordinal LIMIT 10001;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.ingest_decode_outcomes(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ingest_decode_outcomes(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.ingest_decode_outcomes(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
