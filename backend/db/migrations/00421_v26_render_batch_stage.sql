-- +goose Up
-- Aggregate display stage is separate from durable parent terminal status.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_batch_stage(p_tenant uuid,p_batch uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo SET jit=off AS $$
 WITH parent AS MATERIALIZED (
 SELECT r.* FROM photo.runs r JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active' WHERE r.tenant_id=p_tenant AND r.id=p_batch
  AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.kind='bulk' AND r.config_snapshot->'schema_version'='1'::jsonb
  AND r.config_snapshot ?& ARRAY['policy_sha','auth_session_id','parts']
  AND jsonb_typeof(r.config_snapshot->'parts')='array'
 ), parts AS MATERIALIZED (
 SELECT parent.status,parent.cancel_requested,child.status AS child_status,child.graph_sealed
 FROM parent CROSS JOIN LATERAL jsonb_array_elements(parent.config_snapshot->'parts') part
 LEFT JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id::text=part->>'run_id'
  AND child.kind='v2' AND child.created_by=parent.created_by AND child.input_snapshot_id=parent.input_snapshot_id
  AND child.config_hash=part->>'config_sha'
  AND child.config_snapshot->>'project_id'=parent.config_snapshot->>'project_id'
  AND child.config_snapshot->'v26_render'->>'auth_session_id'=parent.config_snapshot->>'auth_session_id'
 )
 SELECT CASE
  WHEN parent.status IN('completed','completed_with_errors','failed','cancelled','cancelling') THEN parent.status
  WHEN parent.cancel_requested THEN 'cancelling'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status IS NULL OR (child_status IN('draft','queued','running','waiting_review') AND NOT graph_sealed)) THEN 'preparing'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='waiting_review' AND graph_sealed) THEN 'waiting_review'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='running' AND graph_sealed) THEN 'running'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='queued') THEN 'queued'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='cancelling') THEN 'cancelling'
  WHEN EXISTS(SELECT 1 FROM parts) AND NOT EXISTS(SELECT 1 FROM parts WHERE child_status NOT IN('completed','completed_with_errors','failed','cancelled')) THEN 'finalizing'
  ELSE 'preparing' END
 FROM parent;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_stage(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_stage(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_batch_stage(uuid,uuid) TO photo_api,photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
