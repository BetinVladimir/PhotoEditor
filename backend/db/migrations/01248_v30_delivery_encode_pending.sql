-- +goose Up
-- Candidate discovery is bounded; each adoption must reread its exact context.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_pending_encodes(p_limit integer)
RETURNS TABLE(tenant_id uuid,task_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.id FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id
 WHERE p_limit BETWEEN 1 AND 64 AND t.operation='export.encode' AND t.operation_version='1'
 AND t.resource_class='cpu' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.canonical_result_id IS NOT NULL AND t.execution_profile->>'renderer_version'='v2.2'
 AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot ? 'v30_delivery_export' AND tenant.status='active'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 ORDER BY t.created_at,t.id LIMIT CASE WHEN p_limit BETWEEN 1 AND 64 THEN p_limit ELSE 0 END;
$$;
ALTER FUNCTION photo.v30_delivery_pending_encodes(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_pending_encodes(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_pending_encodes(integer) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
