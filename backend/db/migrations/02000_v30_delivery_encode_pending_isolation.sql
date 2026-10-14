-- +goose Up
-- Delivery's final export.encode and metadata.write have separate physical
-- verifiers and fenced adoption paths. Neither may enter generic V2 adoption.
-- Other tasks in the Delivery child graph retain ordinary V2 adoption.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_pending_adoptions()
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE NOT (r.config_snapshot ? 'v29_perspective_replacement_detection_jcs')
 AND NOT (r.config_snapshot ? 'v30_delivery_export'
  AND t.operation IN('export.encode','metadata.write'))
 AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND tenant.status='active' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.canonical_result_id IS NOT NULL AND t.execution_profile->>'renderer_version'='v2.2'
 AND w.renderer_version='v2.2'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
  AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 ORDER BY t.created_at,t.id LIMIT 64;
$$;
ALTER FUNCTION photo.v2_pending_adoptions() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_adoptions() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_pending_adoptions() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
