-- +goose Up
-- The generic run-output query must not publish a reviewable task's canonical
-- candidate while its review is pending or selects the base/skip route.
-- +goose StatementBegin
CREATE FUNCTION photo.candidate_review_canonical_output_allowed(
 p_actor uuid,p_session uuid,p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authority
  JOIN photo.tasks task ON task.tenant_id=p_tenant AND task.id=p_task
  WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
   AND (
    task.operation NOT IN ('render.compose','optical.negative')
    OR NOT EXISTS(
     SELECT 1 FROM photo.reviews review
     WHERE review.tenant_id=task.tenant_id AND review.task_id=task.id
      AND (
       CASE task.operation
        WHEN 'render.compose' THEN
         review.kind='render.compose' AND review.state='decided'
         AND review.decision='accept' AND review.decision_route='candidate'
         AND photo.v26_render_review_completion_ready(task.tenant_id,task.id)
        WHEN 'optical.negative' THEN
         review.kind='optical.negative' AND review.state='decided'
         AND review.decision='accept' AND review.decision_route='candidate'
         AND photo.v29_negative_review_completion_ready(task.tenant_id,task.id)
        ELSE false
       END
      ) IS NOT TRUE)
    )
   )
 );
$$;
ALTER FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker;
GRANT EXECUTE ON FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
