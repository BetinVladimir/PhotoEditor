-- +goose Up
-- Count only tasks declared in the exact immutable child plan.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_batch_parts(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(part_ordinal bigint,run_id uuid,first_ordinal integer,frame_count integer,run_status text,
 task_count bigint,succeeded_count bigint,failed_count bigint,cancelled_count bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT part.ordinality-1,(part.value->>'run_id')::uuid,(part.value->>'first_ordinal')::integer,
  (part.value->>'frame_count')::integer,coalesce(child.status,'missing'),count(t.id),
  count(t.id) FILTER(WHERE t.state='succeeded'),count(t.id) FILTER(WHERE t.state='failed'),
  count(t.id) FILTER(WHERE t.state='cancelled')
 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session) parent
 CROSS JOIN LATERAL jsonb_array_elements(parent.manifest->'parts') WITH ORDINALITY part
 LEFT JOIN photo.runs child ON child.tenant_id=p_tenant AND child.id=(part.value->>'run_id')::uuid
  AND child.kind='v2' AND child.created_by=p_actor AND child.config_hash=part.value->>'config_sha'
  AND child.input_snapshot_id=(parent.manifest->>'input_snapshot_id')::uuid
  AND child.config_snapshot->>'project_id'=parent.manifest->>'project_id'
  AND child.config_snapshot->'v26_render'->>'auth_session_id'=p_session::text
 LEFT JOIN photo.tasks t ON t.tenant_id=child.tenant_id AND t.run_id=child.id
  AND t.operation='render.compose' AND t.operation_version='1'
  AND EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(child.config_snapshot->'v26_render'->'nodes')='array' THEN child.config_snapshot->'v26_render'->'nodes' ELSE '[]'::jsonb END) planned
   WHERE planned->>'task_id'=t.id::text AND planned->'params'->>'artifact_id'=t.params_artifact_id::text
    AND planned->>'input_digest'=t.input_digest)
 GROUP BY part.ordinality,part.value,child.status ORDER BY part.ordinality;
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
