-- +goose Up
-- Scheduler-only, read-only context for compiling download export tasks.
-- +goose StatementBegin
CREATE FUNCTION photo.download_export_context(p_tenant uuid,p_run uuid)
RETURNS TABLE(job_id uuid,snapshot_id uuid,snapshot_digest text,item_count bigint,preset jsonb,ordinal bigint,
 asset_version_id uuid,master_id uuid,master_sha text,master_bucket text,master_key text,master_version text,master_bytes bigint,master_media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT j.id,s.id,s.digest,s.item_count,j.preset,si.ordinal,si.asset_version_id,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.runs r
 JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id AND j.snapshot_id=r.input_snapshot_id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id AND s.kind='selection'
 JOIN photo.snapshot_items si ON si.tenant_id=s.tenant_id AND si.snapshot_id=s.id
 JOIN photo.artifacts a ON a.tenant_id=si.tenant_id AND a.id=si.artifact_id AND a.sha256=si.sha256
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='download' AND r.status IN ('queued','running')
 AND NOT r.cancel_requested AND NOT j.cancel_requested AND j.status IN ('queued','preparing')
 AND s.principal_type=j.principal_type AND s.principal_id=j.principal_id
 AND s.item_count BETWEEN 1 AND 5000 AND (s.expires_at IS NULL OR s.expires_at>clock_timestamp())
 AND a.state='ready' AND a.object_version IS NOT NULL AND a.object_version<>'' AND a.object_version<>'null'
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY si.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_export_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_export_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_export_context(uuid,uuid) TO photo_scheduler;

-- Nested refs are accepted only when they are canonical outputs of the exact
-- task that produced the version master manifest.
-- +goose StatementBegin
CREATE FUNCTION photo.download_export_inputs(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal bigint,role text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT si.ordinal,a.output_role,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r
 JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id AND j.snapshot_id=r.input_snapshot_id
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id
 JOIN photo.artifacts master ON master.tenant_id=si.tenant_id AND master.id=si.artifact_id AND master.sha256=si.sha256
 JOIN photo.tasks t ON t.tenant_id=master.tenant_id AND t.id=master.created_by_task_id AND t.state='succeeded' AND t.operation='asset.decode'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role IN ('working_rgb','alpha')
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='download' AND r.status IN ('queued','running')
 AND NOT r.cancel_requested AND NOT j.cancel_requested AND j.status IN ('queued','preparing')
 AND master.state='ready' AND master.output_role='working_image'
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 AND a.state='ready' AND a.object_version IS NOT NULL AND a.object_version<>'' AND a.object_version<>'null'
 AND a.media_type='application/octet-stream' AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY si.ordinal,a.output_role,a.id
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_export_inputs(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_export_inputs(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_export_inputs(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
