-- +goose Up
-- Scheduler-only bounded discovery; no object locators or principal data returned.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v25_lens_preview_tenant(p_after uuid)
RETURNS TABLE(tenant_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT DISTINCT r.tenant_id FROM photo.runs r
 WHERE r.v25_lens_preview_task_id IS NOT NULL AND r.kind='preview'
  AND (p_after IS NULL OR r.tenant_id>p_after)
  AND ((r.status IN('draft','running') AND NOT r.cancel_requested)
   OR (r.status IN('completed','completed_with_errors','failed','cancelled') AND EXISTS(
    SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved')))
 ORDER BY r.tenant_id LIMIT 1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.next_v25_lens_preview_tenant(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v25_lens_preview_tenant(uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.next_v25_lens_preview_tenant(uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
