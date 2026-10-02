-- +goose Up
-- Read-only counts use the same tenant context as the existing run-status API.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_batch_counts(p_tenant uuid,p_batch uuid)
RETURNS TABLE(total bigint,succeeded bigint,failed bigint,skipped bigint,review bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo SET jit=off AS $$
 WITH parent AS MATERIALIZED (
  SELECT r.* FROM photo.runs r JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
  WHERE r.tenant_id=p_tenant AND r.id=p_batch AND p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
   AND r.kind='bulk' AND r.config_snapshot->'schema_version'='1'::jsonb
   AND r.config_snapshot ?& ARRAY['policy_sha','auth_session_id','parts']
   AND jsonb_typeof(r.config_snapshot->'parts')='array'
 ), parts AS MATERIALIZED (
  SELECT parent.id,parent.status,parent.tenant_id,parent.created_by,parent.input_snapshot_id,parent.config_snapshot,
   part.value FROM parent CROSS JOIN LATERAL jsonb_array_elements(parent.config_snapshot->'parts') part
 ), children AS MATERIALIZED (
  SELECT DISTINCT child.id,child.tenant_id,child.config_snapshot FROM parts
  JOIN photo.runs child ON child.tenant_id=parts.tenant_id AND child.id::text=parts.value->>'run_id'
   AND child.kind='v2' AND child.created_by=parts.created_by AND child.input_snapshot_id=parts.input_snapshot_id
   AND child.config_hash=parts.value->>'config_sha'
   AND child.config_snapshot->>'project_id'=parts.config_snapshot->>'project_id'
   AND child.config_snapshot->'v26_render'->>'auth_session_id'=parts.config_snapshot->>'auth_session_id'
 ), planned AS MATERIALIZED (
  SELECT child.id,child.tenant_id,node.value FROM children child
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(child.config_snapshot->'v26_render'->'nodes')='array'
   THEN child.config_snapshot->'v26_render'->'nodes' ELSE '[]'::jsonb END) node
 ), tasks AS MATERIALIZED (
  SELECT DISTINCT t.id,t.state FROM planned JOIN photo.tasks t ON t.tenant_id=planned.tenant_id AND t.run_id=planned.id
   AND t.id::text=planned.value->>'task_id' AND t.operation='render.compose' AND t.operation_version='1'
   AND t.params_artifact_id::text=planned.value->'params'->>'artifact_id' AND t.input_digest=planned.value->>'input_digest'
 ), counts AS MATERIALIZED (
  SELECT count(*) FILTER(WHERE state='succeeded') AS succeeded,
   count(*) FILTER(WHERE state IN('failed','cancelled')) AS failed,
   count(*) FILTER(WHERE state='skipped') AS skipped FROM tasks
 ), expected AS MATERIALIZED (SELECT coalesce(sum((value->>'frame_count')::bigint),0)::bigint AS total FROM parts)
 SELECT expected.total,counts.succeeded,
  CASE WHEN parent.status='cancelled' THEN greatest(counts.failed,expected.total-counts.succeeded-counts.skipped) ELSE counts.failed END,
  counts.skipped,(SELECT count(*) FROM photo.reviews r JOIN children ON r.tenant_id=children.tenant_id AND r.run_id=children.id WHERE r.state='pending')
 FROM parent CROSS JOIN expected CROSS JOIN counts;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_counts(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_counts(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_batch_counts(uuid,uuid) TO photo_api,photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
