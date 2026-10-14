-- +goose Up
-- Scheduling metadata only: never grants task/source/consent authority.
-- The claim survives scheduler crashes. 31 minutes exceeds the processor's
-- maximum 30-minute attempt; expired claims remain fully revalidated.
-- +goose StatementBegin
ALTER TABLE photo.v2_workflows ADD COLUMN v30_import_retry_after timestamptz;
GRANT UPDATE(v30_import_retry_after) ON photo.v2_workflows TO photo_fn_owner;
CREATE INDEX v2_workflows_v30_import_retry ON photo.v2_workflows(v30_import_retry_after,id) WHERE workflow_type='interchange';
-- Malformed persisted discovery metadata is ineligible, never a queue-wide
-- cast exception. This helper is private to the definer, not a public parser.
CREATE FUNCTION photo.v30_import_discovery_uuid(value text) RETURNS uuid
LANGUAGE sql IMMUTABLE STRICT SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN octet_length(value)=36 AND value ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'
 THEN value::uuid ELSE NULL::uuid END
$$;
ALTER FUNCTION photo.v30_import_discovery_uuid(text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_discovery_uuid(text) FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal;
CREATE FUNCTION photo.v30_claim_next_interchange_import()
RETURNS TABLE(tenant_id uuid,workflow_id uuid,run_id uuid,project_id uuid,snapshot_digest text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidate AS MATERIALIZED (
 SELECT w.tenant_id,w.id AS workflow_id,w.run_id,w.project_id,r.config_snapshot->>'snapshot_digest' AS snapshot_digest,a.id AS artifact_id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version
 FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.tenants tenant ON tenant.id=w.tenant_id AND tenant.status='active'
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL AND p.download_allowed
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation='v2.interchange.parse' AND t.state='succeeded' AND NOT t.cancel_requested
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id AND d.run_id=t.run_id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=photo.v30_import_discovery_uuid(z.outputs->0->>'artifact_id') AND a.sha256=z.outputs->0->>'sha256'
 WHERE (w.v30_import_retry_after IS NULL OR w.v30_import_retry_after<=clock_timestamp()) AND w.workflow_type='interchange' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes BETWEEN 2 AND 8388608 AND a.media_type='application/json'
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND photo.v2_editor_authorized(w.tenant_id,w.project_id,r.created_by,photo.v30_import_discovery_uuid(d.v30_interchange_proof->>'auth_session_id'))
 AND photo.v30_interchange_parse_adoption_committed(w.tenant_id,t.id)
 AND EXISTS(SELECT 1 FROM photo.collections c WHERE c.tenant_id=w.tenant_id AND c.id=photo.v30_uuid5(w.id,'interchange:draft-collection:1')
 AND c.project_id=w.project_id AND c.source_run_id=w.run_id AND c.state='draft' AND c.deleted_at IS NULL)
 AND coalesce(r.config_snapshot->>'snapshot_digest','') ~ '^[0-9a-f]{64}$'
 AND NOT EXISTS(SELECT 1 FROM photo.tasks pending WHERE pending.tenant_id=w.tenant_id AND pending.run_id=w.run_id
 AND pending.id=photo.v30_uuid5(w.id,'v2.interchange.import:0')
 AND (pending.state<>'blocked' OR pending.attempt<>0 OR pending.cancel_requested OR pending.terminal_reason IS NOT NULL OR pending.v30_import_activation IS NOT NULL))
 AND NOT EXISTS(SELECT 1 FROM photo.runs child WHERE child.tenant_id=w.tenant_id AND child.id=photo.v30_uuid5(w.id,'interchange:canonical-ingest:1')
 AND (child.status NOT IN('completed','completed_with_errors','failed') OR child.cancel_requested))
 ORDER BY coalesce(w.v30_import_retry_after,'-infinity'::timestamptz),r.created_at,w.id LIMIT 1 FOR UPDATE OF w SKIP LOCKED
 ), claimed AS (
 UPDATE photo.v2_workflows w SET v30_import_retry_after=clock_timestamp()+interval '31 minutes'
 FROM candidate c WHERE w.tenant_id=c.tenant_id AND w.id=c.workflow_id
 RETURNING w.tenant_id,w.id
 )
 SELECT c.* FROM candidate c JOIN claimed k ON k.tenant_id=c.tenant_id AND k.id=c.workflow_id
$$;
ALTER FUNCTION photo.v30_claim_next_interchange_import() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_claim_next_interchange_import() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_claim_next_interchange_import() TO photo_scheduler;
-- Prevent old scheduler binaries from bypassing the durable retry claim.
REVOKE EXECUTE ON FUNCTION photo.v30_next_interchange_import() FROM photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
