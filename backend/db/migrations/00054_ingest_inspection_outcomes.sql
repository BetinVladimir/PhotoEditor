-- +goose Up
-- Canonical inspection outcomes plus version-pinned metadata locations.
-- +goose StatementBegin
CREATE FUNCTION photo.ingest_inspection_outcomes(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal bigint,verify_state text,inspect_state text,inspect_reason text,source_id uuid,source_sha text,metadata jsonb)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,v.state,t.state,t.terminal_reason,n.source_artifact_id,n.source_sha,
 CASE WHEN t.state='succeeded' AND m.count=1 THEN m.docs->0 ELSE NULL END
 FROM photo.runs r JOIN photo.ingest_progress progress ON progress.tenant_id=r.tenant_id AND progress.run_id=r.id
 JOIN photo.ingest_node_plans n ON n.tenant_id=r.tenant_id AND n.run_id=r.id AND n.operation='asset.inspect'
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
 JOIN photo.ingest_node_plans vn ON vn.tenant_id=n.tenant_id AND vn.run_id=n.run_id AND vn.ordinal=n.ordinal AND vn.operation='asset.verify'
 JOIN photo.tasks v ON v.tenant_id=vn.tenant_id AND v.run_id=vn.run_id AND v.id=vn.id
 LEFT JOIN LATERAL (
  SELECT count(*) count,jsonb_agg(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type)) docs
  FROM photo.task_results z JOIN photo.artifacts a ON a.tenant_id=z.tenant_id AND a.created_by_task_id=z.task_id
  WHERE z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id AND a.state='ready' AND a.output_role='metadata'
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 ) m ON true
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='ingest' AND r.status IN ('queued','running')
 AND NOT r.cancel_requested AND progress.phase='inspection'
 ORDER BY n.ordinal;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.ingest_inspection_outcomes(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ingest_inspection_outcomes(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.ingest_inspection_outcomes(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
