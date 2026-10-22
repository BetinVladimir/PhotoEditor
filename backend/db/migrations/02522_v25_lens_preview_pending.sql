-- +goose Up
-- Tenant-scoped keyset observation, never an adoption authority.
-- +goose StatementBegin
CREATE FUNCTION photo.pending_v25_lens_preview_receipts(p_tenant uuid,p_after uuid)
RETURNS TABLE(run_id uuid,variant text,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id,d.v25_lens_preview_params::jsonb->>'variant',t.id
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
 CROSS JOIN LATERAL photo.v25_lens_preview_context(p_tenant,r.id,d.v25_lens_preview_params::jsonb->>'variant') c
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND r.tenant_id=p_tenant
  AND r.v25_lens_preview_task_id IS NOT NULL AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
  AND d.purpose='task_params' AND d.state='ready' AND d.v25_lens_preview_receipt IS NULL
  AND c.task_id=t.id AND (p_after IS NULL OR t.id>p_after)
 ORDER BY t.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_v25_lens_preview_receipts(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v25_lens_preview_receipts(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.pending_v25_lens_preview_receipts(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
