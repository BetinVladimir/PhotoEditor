-- +goose Up
-- Recovery after receipt commit but before completion; tenant-scoped bounded scan.
-- +goose StatementBegin
CREATE FUNCTION photo.pending_v25_lens_preview_completion(p_tenant uuid,p_after uuid)
RETURNS TABLE(run_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id FROM photo.runs r
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND r.tenant_id=p_tenant
  AND r.v25_lens_preview_task_id IS NOT NULL AND r.kind='preview'
  AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
  AND (p_after IS NULL OR r.id>p_after)
  AND (SELECT count(*) FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id
   AND d.purpose='task_params' AND d.state='ready' AND d.v25_lens_preview_receipt IS NOT NULL)=2
 ORDER BY r.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_v25_lens_preview_completion(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v25_lens_preview_completion(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.pending_v25_lens_preview_completion(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
