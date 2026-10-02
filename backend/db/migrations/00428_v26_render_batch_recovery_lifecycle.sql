-- +goose Up
-- Recovery publication is only eligible while the parent lifecycle is active.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_batch_part_state(p_tenant uuid,p_batch uuid,p_actor uuid,p_session uuid,p_ordinal integer)
RETURNS TABLE(run_id uuid,workflow_id uuid,project_id uuid,snapshot_id uuid,config jsonb,config_sha text,policy_sha text,reserved_bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT (part.body->>'run_id')::uuid,child.workflow_id,child.project_id,child.snapshot_id,child.config,
  child.config_sha,child.policy_sha,child.reserved_bytes
 FROM photo.v26_render_batch_manifest(p_tenant,p_batch,p_actor,p_session) parent
 JOIN photo.runs parent_run ON parent_run.tenant_id=p_tenant AND parent_run.id=p_batch
  AND parent_run.config_hash=parent.manifest_sha AND NOT parent_run.cancel_requested
  AND parent_run.status IN('draft','queued','running','waiting_review')
 CROSS JOIN LATERAL (SELECT parent.manifest->'parts'->p_ordinal AS body) part
 CROSS JOIN LATERAL photo.v26_render_draft_state(p_tenant,(part.body->>'run_id')::uuid,p_actor,p_session) child
 WHERE p_ordinal BETWEEN 0 AND 4999 AND NOT parent.cancel_requested
  AND child.project_id=(parent.manifest->>'project_id')::uuid
  AND child.snapshot_id=(parent.manifest->>'input_snapshot_id')::uuid
  AND child.config_sha=part.body->>'config_sha' AND child.policy_sha=parent.manifest->>'policy_sha'
  AND child.reserved_bytes=(part.body->>'output_bytes')::bigint
  AND jsonb_array_length(child.config->'v26_render'->'nodes')=(part.body->>'frame_count')::integer;
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
