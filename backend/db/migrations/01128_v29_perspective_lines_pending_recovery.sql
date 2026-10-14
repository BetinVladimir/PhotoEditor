-- +goose Up
-- One read-only candidate per call. Activation remains tenant scoped and locked.
CREATE INDEX runs_v29_lines_pending_recovery ON photo.runs(id)
 WHERE v29_perspective_line_detection AND status='draft' AND NOT graph_sealed
 AND NOT cancel_requested AND v29_perspective_lines_seal_receipt IS NOT NULL;
-- +goose StatementBegin
CREATE FUNCTION photo.next_v29_perspective_lines_recovery(p_after uuid)
RETURNS TABLE(tenant_id uuid,run_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 WHERE r.v29_perspective_line_detection AND r.status='draft' AND NOT r.graph_sealed
 AND NOT r.cancel_requested AND r.v29_perspective_lines_seal_receipt IS NOT NULL
 AND r.v29_perspective_lines_execution_policy IS NOT NULL
 AND (p_after IS NULL OR r.id>p_after)
 AND EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation='optical.perspective_lines'
 AND op.version='1' AND op.v2_enabled)
 ORDER BY r.id LIMIT 1
$$;
ALTER FUNCTION photo.next_v29_perspective_lines_recovery(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v29_perspective_lines_recovery(uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.next_v29_perspective_lines_recovery(uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
