-- +goose Up
-- Omission is a result of an exact settled skip closure, never task state alone.
-- This changes review selection only. Predecessor adoption/failure/cancellation,
-- quota, event and operation-specific finalization barriers remain in force.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_skip_task_omitted(p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
 SELECT 1 FROM photo.v29_negative_invalidations i
 JOIN photo.v29_negative_skip_closures c ON c.tenant_id=i.tenant_id AND c.review_id=i.review_id
 AND c.origin_task_id=i.origin_task_id AND c.decision_hash=i.decision_hash
 JOIN photo.tasks origin ON origin.tenant_id=c.tenant_id AND origin.id=c.origin_task_id
 JOIN photo.tasks dependent ON dependent.tenant_id=i.tenant_id AND dependent.id=i.task_id
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.route='skip'
 AND dependent.id<>origin.id AND dependent.run_id=origin.run_id
 AND dependent.state IN('succeeded','failed','cancelled','skipped')
 AND c.plan->>'origin_run_id'=origin.run_id::text
 AND c.plan->'affected_tasks' @> jsonb_build_array(dependent.id::text)
 AND photo.v29_negative_skip_closure_ready(p_tenant,origin.id))
$$;
ALTER FUNCTION photo.v29_negative_skip_task_omitted(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_skip_task_omitted(uuid,uuid) FROM PUBLIC;

DO $$
DECLARE definition text; old_gate text; new_gate text; signature text; task_alias text; review_alias text;
BEGIN
 FOREACH signature IN ARRAY ARRAY['photo.finalize_v2_run(uuid,uuid)','photo.v2_pending_finalizations()'] LOOP
  definition:=pg_get_functiondef(signature::regprocedure);
  task_alias:=CASE WHEN signature='photo.finalize_v2_run(uuid,uuid)' THEN 'task' ELSE 'rt' END;
  review_alias:=CASE WHEN signature='photo.finalize_v2_run(uuid,uuid)' THEN 'v' ELSE 'rv' END;
  -- Both existence and unresolved-review checks use this exact run predicate.
  old_gate:=format('%s.tenant_id=%s AND %s.run_id=%s',task_alias,
   CASE WHEN task_alias='task' THEN 'p_tenant' ELSE 'r.tenant_id' END,task_alias,
   CASE WHEN task_alias='task' THEN 'p_run' ELSE 'r.id' END);
  new_gate:=old_gate||format(' AND NOT photo.v29_negative_skip_task_omitted(%s.tenant_id,%s.id)',task_alias,task_alias);
  IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_TASK_REVIEW_PATCH_MISMATCH';END IF;
  definition:=replace(definition,old_gate,new_gate);
  old_gate:=format('%s.tenant_id=%s AND %s.run_id=%s',review_alias,
   CASE WHEN review_alias='v' THEN 'p_tenant' ELSE 'r.tenant_id' END,review_alias,
   CASE WHEN review_alias='v' THEN 'p_run' ELSE 'r.id' END);
  new_gate:=old_gate||format(' AND NOT photo.v29_negative_skip_task_omitted(%s.tenant_id,%s.task_id)',review_alias,review_alias);
  IF strpos(definition,old_gate)=0 THEN RAISE EXCEPTION 'NEGATIVE_SKIP_EXISTING_REVIEW_PATCH_MISMATCH';END IF;
  definition:=replace(definition,old_gate,new_gate);
  EXECUTE definition;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
