-- +goose Up
-- Principal-scoped projection of the scheduler-only render selection. This
-- function returns no object address and does not grant API callers direct
-- execution of v26_render_review_selection.
-- +goose StatementBegin
CREATE FUNCTION photo.get_v26_render_review_selection(
 p_tenant uuid,p_actor uuid,p_session uuid,p_review uuid)
RETURNS TABLE(review_id uuid,decision_hash text,route text,image_id uuid,image_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT selected.review_id,selected.decision_hash,selected.route,
        selected.image_id,selected.image_sha
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authority
 JOIN photo.reviews v ON v.tenant_id=p_tenant AND v.id=p_review AND v.kind='render.compose'
 JOIN photo.tasks task ON task.tenant_id=v.tenant_id AND task.id=v.task_id AND task.run_id=v.run_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=task.tenant_id AND workflow.run_id=task.run_id
 JOIN photo.projects project ON project.tenant_id=workflow.tenant_id
  AND project.id=workflow.project_id AND project.archived_at IS NULL
 CROSS JOIN LATERAL photo.v26_render_review_selection(p_tenant,v.task_id) selected
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND authority.role IN('owner','admin','editor')
  AND (authority.role IN('owner','admin') OR project.visibility='workspace' OR EXISTS(
   SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant
    AND acl.project_id=project.id AND acl.user_id=p_actor
    AND acl.role IN('owner','admin','editor')))
  AND selected.review_id=p_review
  AND (SELECT count(*) FROM photo.v2_workflows singleton
   WHERE singleton.tenant_id=task.tenant_id AND singleton.run_id=task.run_id)=1
$$;
ALTER FUNCTION photo.get_v26_render_review_selection(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v26_render_review_selection(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_worker,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.get_v26_render_review_selection(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
