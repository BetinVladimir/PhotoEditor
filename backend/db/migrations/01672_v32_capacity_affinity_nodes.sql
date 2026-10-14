-- +goose Up
-- Cache hints only reorder already eligible nodes for the exact selected task.
-- Unknown operation/model identity retains original eligible-node ordering.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_affinity_dispatch_nodes(p_task uuid) RETURNS TABLE(node_id text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH selected AS (
 SELECT t.*,CASE t.operation WHEN 'face.detect' THEN 'sfd' WHEN 'face.landmarks' THEN 'fan2d'
 WHEN 'face.parse' THEN 'bisenet_face19' WHEN 'mask.person' THEN 'lraspp_mobilenet_v3_large' END AS model_id
 FROM photo.tasks t WHERE t.id=p_task
 ), eligible AS (
 SELECT e.node_id,e.ordinality FROM photo.eligible_dispatch_nodes(p_task) WITH ORDINALITY e
 )
 SELECT e.node_id FROM eligible e CROSS JOIN selected t
 ORDER BY EXISTS(
 SELECT 1 FROM photo.worker_registrations w JOIN photo.capacity_policy_timings c ON c.policy_sha=w.capacity_policy_sha
 CROSS JOIN LATERAL jsonb_array_elements(w.model_residency) m
 WHERE w.node_id=e.node_id AND w.tenant_id=t.tenant_id AND w.enabled
 AND w.resource_classes ? t.resource_class AND w.backends ? (t.execution_profile->>'backend')
 AND (w.qualified_operations->t.operation) ? (t.execution_profile->>'backend')
 AND photo.capacity_policy_worker_fresh(w)
 AND w.model_residency_reported_at<=clock_timestamp()
 AND extract(epoch FROM clock_timestamp()-w.model_residency_reported_at)<c.offline_seconds
 AND m->>'model_id'=t.model_id AND m->>'model_lock_sha'=t.execution_profile->>'model_lock_sha'
 AND m->>'backend'=t.execution_profile->>'backend'
 AND m->>'qualification_report_sha' ~ '^[0-9a-f]{64}$'
 ) DESC,e.ordinality;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_affinity_dispatch_nodes(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_affinity_dispatch_nodes(uuid) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_affinity_dispatch_nodes(uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
