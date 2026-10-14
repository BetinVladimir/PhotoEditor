-- +goose Up
-- Global tenant discovery reveals only an active tenant id to the scheduler.
-- The tenant-scoped queue repeats current review/result authority.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v28_restoration_preview_receipt_tenant(p_after uuid)
RETURNS TABLE(tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE r.v28_restoration_preview_task_id IS NOT NULL AND r.kind='preview'
  AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
  AND (p_after IS NULL OR r.tenant_id>p_after)
  AND (EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.backend_documents d
   ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
   AND d.purpose='task_params' AND d.state='ready'
   WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state='succeeded'
    AND t.operation='media.preview' AND NOT t.cancel_requested
    AND d.v28_restoration_preview_params IS NOT NULL
    AND d.v28_restoration_preview_derivative_receipt IS NULL)
   OR (SELECT count(*) FROM photo.backend_documents d
    WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND d.purpose='task_params'
     AND d.state='ready' AND d.v28_restoration_preview_params IS NOT NULL
     AND d.v28_restoration_preview_derivative_receipt IS NOT NULL)=4)
 GROUP BY r.tenant_id ORDER BY r.tenant_id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v28_restoration_preview_receipt_tenant(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v28_restoration_preview_receipt_tenant(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.next_v28_restoration_preview_receipt_tenant(uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.pending_v28_restoration_preview_receipts(p_tenant uuid,p_after uuid)
RETURNS TABLE(run_id uuid,side text,variant text,task_id uuid)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id,parts.side,parts.variant,t.id
 FROM photo.runs r
 CROSS JOIN LATERAL photo.v28_restoration_preview_source(
  p_tenant,r.v28_restoration_preview_task_id) s
 CROSS JOIN LATERAL (VALUES ('before','preview1600'),('before','preview2560'),
  ('after','preview1600'),('after','preview2560')) parts(side,variant)
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.node_key='restoration.'||parts.side||':'||parts.variant
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id
  AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id
  AND d.purpose='task_params' AND d.state='ready'
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.tenant_id=p_tenant AND r.v28_restoration_preview_task_id IS NOT NULL
  AND r.kind='preview' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.config_snapshot->>'source_review_id'=s.review_id::text
  AND r.config_snapshot->>'source_result_id'=s.result_id::text
  AND r.config_snapshot->>'source_attempt'=s.attempt::text
  AND r.config_snapshot->>'source_fence'=s.fence::text
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
  AND r.config_snapshot->'before'=jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)
  AND r.config_snapshot->'after'=jsonb_build_object('artifact_id',s.candidate_id,'sha256',s.candidate_sha)
  AND t.state='succeeded' AND NOT t.cancel_requested AND t.operation='media.preview'
  AND t.operation_version='1' AND d.v28_restoration_preview_params IS NOT NULL
  AND d.v28_restoration_preview_derivative_receipt IS NULL
  AND (p_after IS NULL OR t.id>p_after)
  AND (SELECT count(*) FROM photo.v28_restoration_preview_result(
   p_tenant,r.id,parts.side,parts.variant))=2
 ORDER BY t.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_v28_restoration_preview_receipts(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v28_restoration_preview_receipts(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.pending_v28_restoration_preview_receipts(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
