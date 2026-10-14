-- +goose Up
-- A decided render review does not change the producer's immutable result.
-- Reject/skip may finish only when no task or content consumer has already
-- selected any artifact produced by that task. Exact preview derivatives are
-- read-only observations and do not publish a replacement result.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_review_completion_ready(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.v26_render_review_selection(p_tenant,p_task) selected
  JOIN photo.tasks origin ON origin.tenant_id=p_tenant AND origin.id=p_task
  WHERE selected.route='candidate' OR (
   selected.route IN('base','skip')
   AND NOT EXISTS(
    SELECT 1 FROM photo.task_edges edge
    WHERE edge.tenant_id=p_tenant AND edge.prerequisite_id=p_task)
   AND NOT EXISTS(
    SELECT 1 FROM photo.artifacts output
    JOIN photo.tasks consumer ON consumer.tenant_id=output.tenant_id AND consumer.id<>p_task
    JOIN photo.runs consumer_run ON consumer_run.tenant_id=consumer.tenant_id
     AND consumer_run.id=consumer.run_id
    WHERE output.tenant_id=p_tenant AND output.created_by_task_id=p_task
     AND NOT (consumer_run.kind='preview'
      AND consumer_run.v26_derivative_task_id IS NOT DISTINCT FROM p_task
      AND consumer_run.config_snapshot->>'source_result_id'
       IS NOT DISTINCT FROM origin.canonical_result_id::text)
     AND (consumer.inputs @> jsonb_build_array(jsonb_build_object(
       'artifact_id',output.id::text,'sha256',output.sha256))
      OR EXISTS(SELECT 1 FROM photo.v2_task_inputs input
       WHERE input.tenant_id=consumer.tenant_id AND input.task_id=consumer.id
        AND input.artifact_id=output.id AND input.sha256=output.sha256))))
 );
$$;
ALTER FUNCTION photo.v26_render_review_completion_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_review_completion_ready(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.v26_render_review_completion_ready(uuid,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
