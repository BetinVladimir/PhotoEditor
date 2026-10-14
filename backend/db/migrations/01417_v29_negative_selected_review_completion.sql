-- +goose Up
-- Finalize terminal negative selections without rewriting canonical task output.
-- Downstream invalidation is a separate workflow; a rejected/ skipped producer
-- with dependents cannot complete through this gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_review_completion_ready(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v29_negative_review_selection(p_tenant,p_task) selected
 JOIN photo.tasks origin ON origin.tenant_id=p_tenant AND origin.id=p_task
 WHERE selected.route='candidate' OR (
 selected.route IN('base','skip')
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant AND edge.prerequisite_id=p_task)
 AND NOT EXISTS(SELECT 1 FROM photo.artifacts output JOIN photo.tasks consumer
 ON consumer.tenant_id=output.tenant_id AND consumer.id<>p_task
 JOIN photo.runs consumer_run ON consumer_run.tenant_id=consumer.tenant_id AND consumer_run.id=consumer.run_id
 WHERE output.tenant_id=p_tenant AND output.created_by_task_id=p_task
 -- Read-only derivatives of this exact canonical result do not select/publish
 -- a new content result and must not prevent declining the previewed image.
 AND NOT (consumer_run.kind='preview' AND consumer_run.v26_derivative_task_id IS NOT DISTINCT FROM p_task
 AND consumer_run.config_snapshot->>'source_result_id' IS NOT DISTINCT FROM origin.canonical_result_id::text)
 AND (consumer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',output.id::text,'sha256',output.sha256))
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=consumer.tenant_id AND i.task_id=consumer.id
 AND i.artifact_id=output.id AND i.sha256=output.sha256)))))
$$;
ALTER FUNCTION photo.v29_negative_review_completion_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_review_completion_ready(uuid,uuid) FROM PUBLIC;
DO $$
DECLARE definition text; signature text; alias_name text; negative_gate text; accept_gate text; reject_gate text;
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.finalize_v2_run(uuid,uuid)','photo.v2_pending_finalizations()'] LOOP
  alias_name:=CASE WHEN signature='photo.finalize_v2_run(uuid,uuid)' THEN 'v' ELSE 'rv' END;
  definition:=pg_get_functiondef(signature::regprocedure);
  negative_gate:=format('(%s.kind=''optical.negative'' AND photo.v29_negative_review_completion_ready(%s.tenant_id,%s.task_id))',alias_name,alias_name,alias_name);
  reject_gate:=format('%s.decision IS DISTINCT FROM ''accept'' OR %s.decision_route IS DISTINCT FROM ''candidate''',alias_name,alias_name);
  IF strpos(definition,reject_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SELECTION_REJECT_PATCH_MISMATCH';END IF;
  definition:=replace(definition,reject_gate,format('NOT ((%s.kind=''render.compose'' AND %s.decision=''accept'' AND %s.decision_route=''candidate'') OR %s)',alias_name,alias_name,alias_name,negative_gate));
  accept_gate:=format('%s.decision=''accept'' AND %s.decision_route=''candidate''',alias_name,alias_name);
  -- Includes the newly constructed render-only expression above. Its kind
  -- predicate keeps render behavior unchanged; the per-task check admits the
  -- negative selection only through the authoritative helper.
  IF strpos(definition,accept_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SELECTION_ACCEPT_PATCH_MISMATCH';END IF;
  definition:=replace(definition,accept_gate,format('((%s.kind=''render.compose'' AND %s.decision=''accept'' AND %s.decision_route=''candidate'') OR %s)',alias_name,alias_name,alias_name,negative_gate));
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
