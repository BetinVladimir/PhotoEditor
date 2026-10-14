-- +goose Up
-- Replacement detector adoption requires mapped-source Go/Python bridge.
-- Keep it out of ordinary detector adoption to prevent fallback authority.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_pending_adoptions() RENAME TO v2_pending_adoptions_pre_v29_replacement_detector;
REVOKE ALL ON FUNCTION photo.v2_pending_adoptions_pre_v29_replacement_detector() FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v2_pending_adoptions() RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE NOT (r.config_snapshot ? 'v29_perspective_replacement_detection_jcs') AND r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
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
CREATE FUNCTION photo.next_v29_replacement_detector_adoption(p_after uuid)
RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid,snapshot_id uuid,descriptor_jcs text,
 attempt integer,fence bigint,result_id uuid,root_id uuid,root_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id,t.id,r.input_snapshot_id,r.config_snapshot->>'v29_perspective_replacement_detection_jcs',
 c.attempt,c.fence,c.result_id,c.root_artifact_id,c.root_sha256
 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 CROSS JOIN LATERAL photo.v2_adoption_context(r.tenant_id,t.id) c
 WHERE r.id>p_after AND r.graph_sealed AND NOT r.cancel_requested
 AND r.config_snapshot ? 'v29_perspective_replacement_detection_jcs'
 AND r.v29_replacement_detector_admission_receipt IS NOT NULL
 AND r.v29_replacement_detector_adoption_receipt IS NULL
 AND t.operation='optical.perspective_lines' AND t.operation_version='1'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,c.result_id))
 ORDER BY r.id LIMIT 1;
$$;
ALTER FUNCTION photo.next_v29_replacement_detector_adoption(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v29_replacement_detector_adoption(uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.next_v29_replacement_detector_adoption(uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
