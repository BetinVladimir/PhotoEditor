-- +goose Up
-- Bounded cursor observations; receipt commit repeats locked live authority.
-- +goose StatementBegin
CREATE FUNCTION photo.pending_v26_render_derivative_receipts(p_after uuid)
RETURNS TABLE(tenant_id uuid,run_id uuid,variant text,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id,d.v26_derivative_params::jsonb->>'variant',t.id
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id
 CROSS JOIN LATERAL photo.v26_render_derivative_context(r.tenant_id,r.id,d.v26_derivative_params::jsonb->>'variant') c
 WHERE r.v26_derivative_task_id IS NOT NULL AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND d.purpose='task_params' AND d.state='ready' AND d.v26_derivative_receipt IS NULL AND c.task_id=t.id
 AND (p_after IS NULL OR t.id>p_after) ORDER BY t.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_v26_render_derivative_receipts(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v26_render_derivative_receipts(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pending_v26_render_derivative_receipts(uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
