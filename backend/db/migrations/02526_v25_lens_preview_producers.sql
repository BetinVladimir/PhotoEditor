-- +goose Up
-- Bounded scheduler observation only; Prepare repeats scoped live authority.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v25_lens_preview_producer(p_after uuid,p_srgb text)
RETURNS TABLE(tenant_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.id FROM photo.tasks t JOIN photo.runs parent ON parent.tenant_id=t.tenant_id AND parent.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 WHERE p_srgb ~ '^[0-9a-f]{64}$' AND (p_after IS NULL OR t.id>p_after)
  AND t.operation='optical.lens' AND t.operation_version='1' AND t.state='succeeded'
  AND t.canonical_result_id IS NOT NULL AND NOT t.cancel_requested
  AND parent.kind='v2' AND parent.graph_sealed AND NOT parent.cancel_requested AND parent.status IN('running','waiting_review','completed')
  AND NOT EXISTS(SELECT 1 FROM photo.runs preview WHERE preview.tenant_id=t.tenant_id
   AND preview.v25_lens_preview_task_id=t.id
   AND preview.config_snapshot->>'source_result_id'=t.canonical_result_id::text
   AND preview.config_snapshot->>'srgb_sha256'=p_srgb
   AND (preview.graph_sealed OR preview.cancel_requested OR preview.status<>'draft'))
 ORDER BY t.id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v25_lens_preview_producer(uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v25_lens_preview_producer(uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.next_v25_lens_preview_producer(uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
