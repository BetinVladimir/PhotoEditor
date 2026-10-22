-- +goose Up
-- Principal-scoped boolean gate for generic Outputs reads of a preview run.
-- The API role does not receive the scheduler selection/completion helpers.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_preview_output_allowed(
 p_actor uuid,p_session uuid,p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authority
  JOIN photo.runs preview ON preview.tenant_id=p_tenant AND preview.id=p_run
  WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
   AND (preview.v26_derivative_task_id IS NULL OR EXISTS(
    SELECT 1 FROM photo.tasks origin
    WHERE origin.tenant_id=p_tenant AND origin.id=preview.v26_derivative_task_id
     AND (origin.operation<>'render.compose' OR EXISTS(
      SELECT 1 FROM photo.v2_workflows workflow
      JOIN photo.projects project ON project.tenant_id=workflow.tenant_id
       AND project.id=workflow.project_id AND project.archived_at IS NULL
      WHERE workflow.tenant_id=p_tenant AND workflow.run_id=origin.run_id
       AND (authority.role IN('owner','admin') OR project.visibility='workspace'
        OR EXISTS(SELECT 1 FROM photo.project_acl acl
         WHERE acl.tenant_id=p_tenant AND acl.project_id=project.id
          AND acl.user_id=p_actor AND acl.role IN('owner','admin','editor','viewer')))
       AND NOT EXISTS(SELECT 1 FROM photo.reviews decided
        WHERE decided.tenant_id=p_tenant AND decided.task_id=origin.id
         AND decided.kind='render.compose' AND decided.state='decided'
         AND (decided.decision='accept' AND decided.decision_route='candidate'
          AND photo.v26_render_review_completion_ready(p_tenant,origin.id)) IS NOT TRUE)))))
 );
$$;
ALTER FUNCTION photo.v26_render_preview_output_allowed(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_preview_output_allowed(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker;
GRANT EXECUTE ON FUNCTION photo.v26_render_preview_output_allowed(uuid,uuid,uuid,uuid)
 TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
