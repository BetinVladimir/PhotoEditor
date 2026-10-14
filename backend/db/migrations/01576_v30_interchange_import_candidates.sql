-- +goose Up
-- Repeatable post-collection discovery. No lease grants authority; every
-- compiler/publication/activation stage independently rechecks current scope.
-- Failed publication or blocked registration remains eligible for retry.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_next_interchange_import()
RETURNS TABLE(tenant_id uuid,workflow_id uuid,run_id uuid,project_id uuid,snapshot_digest text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.tenant_id,w.id,w.run_id,w.project_id,r.config_snapshot->>'snapshot_digest',a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version
 FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.tenants tenant ON tenant.id=w.tenant_id AND tenant.status='active'
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL AND p.download_allowed
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation='v2.interchange.parse' AND t.state='succeeded' AND NOT t.cancel_requested
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id AND d.run_id=t.run_id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
 WHERE w.workflow_type='interchange' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 8388608 AND a.media_type='application/json'
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v2_editor_authorized(w.tenant_id,w.project_id,r.created_by,(d.v30_interchange_proof->>'auth_session_id')::uuid)
 AND photo.v30_interchange_parse_adoption_committed(w.tenant_id,t.id)
 AND EXISTS(SELECT 1 FROM photo.collections c WHERE c.tenant_id=w.tenant_id AND c.id=photo.v30_uuid5(w.id,'interchange:draft-collection:1')
 AND c.project_id=w.project_id AND c.source_run_id=w.run_id AND c.state='draft' AND c.deleted_at IS NULL)
 AND coalesce(r.config_snapshot->>'snapshot_digest','') ~ '^[0-9a-f]{64}$'
 AND NOT EXISTS(SELECT 1 FROM photo.tasks pending WHERE pending.tenant_id=w.tenant_id AND pending.run_id=w.run_id
 AND pending.id=photo.v30_uuid5(w.id,'v2.interchange.import:0')
 AND (pending.state<>'blocked' OR pending.attempt<>0 OR pending.cancel_requested OR pending.terminal_reason IS NOT NULL OR pending.v30_import_activation IS NOT NULL))
 AND NOT EXISTS(SELECT 1 FROM photo.runs child WHERE child.tenant_id=w.tenant_id AND child.id=photo.v30_uuid5(w.id,'interchange:canonical-ingest:1')
 AND (child.status NOT IN('completed','completed_with_errors','failed') OR child.cancel_requested))
 ORDER BY r.created_at,w.id LIMIT 1
$$;
ALTER FUNCTION photo.v30_next_interchange_import() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_next_interchange_import() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_next_interchange_import() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
