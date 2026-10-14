-- +goose Up
-- Read-only durable outcome after a lost commit response. No fresh permission.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_adoption_committed(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 JOIN photo.inbox i ON i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='optical.perspective_lines' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND tenant.status='active'
 AND r.v29_perspective_line_detection AND r.graph_sealed AND NOT r.cancel_requested
 AND r.v29_perspective_lines_adoption_receipt->>'task_id'=t.id::text
 AND r.v29_perspective_lines_adoption_receipt->>'result_id'=t.canonical_result_id::text
 AND r.v29_perspective_lines_adoption_receipt->'attempt'=to_jsonb(t.attempt)
 AND r.v29_perspective_lines_adoption_receipt->'fence'=to_jsonb(t.fencing_token)
 AND EXISTS(SELECT 1 FROM photo.run_events e WHERE e.tenant_id=t.tenant_id AND e.run_id=r.id
 AND e.type='v2.task.adopted' AND e.payload->>'task_id'=t.id::text
 AND e.payload->>'result_id'=t.canonical_result_id::text
 AND e.payload->>'inventory_digest'=r.v29_perspective_lines_adoption_receipt->>'inventory_digest'
 AND e.payload->>'sql_digest'=r.v29_perspective_lines_adoption_receipt->>'sql_digest'));
$$;
ALTER FUNCTION photo.v29_perspective_lines_adoption_committed(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_adoption_committed(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_adoption_committed(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
