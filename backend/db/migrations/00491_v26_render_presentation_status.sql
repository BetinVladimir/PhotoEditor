-- +goose Up
-- Presentation is an independent observation; it does not change Render revision.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_presentation_status(p_user uuid,p_session uuid,p_tenant uuid,p_source_run uuid)
RETURNS TABLE(total bigint,pending bigint,ready bigint,failed bigint,cancelled bigint,unavailable bigint)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE role_ref text;project_ref uuid;visibility_ref text;
BEGIN
 SELECT a.role INTO role_ref FROM photo.authorize_tenant(p_user,p_session,p_tenant) a;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT p.id,p.visibility INTO project_ref,visibility_ref FROM photo.v2_workflows w
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL
 WHERE w.tenant_id=p_tenant AND w.run_id=p_source_run AND w.workflow_type='preview';
 IF NOT FOUND THEN RETURN;END IF;
 IF role_ref NOT IN('owner','admin') AND visibility_ref<>'workspace' AND NOT EXISTS(
 SELECT 1 FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=project_ref AND user_id=p_user) THEN RETURN;END IF;
 RETURN QUERY WITH visible AS (
 SELECT o.artifact_id FROM photo.v26_render_derivative_outputs(p_user,p_session,p_tenant,p_source_run) o WHERE o.output_role='derivative'
 ), frames AS (
 SELECT CASE
 WHEN t.state='failed' THEN 'failed'
 WHEN t.state IN('cancelled','skipped') OR t.cancel_requested OR origin.cancel_requested THEN 'cancelled'
 WHEN r.status='failed' THEN 'failed'
 WHEN r.cancel_requested OR r.status IN('cancelling','cancelled') THEN 'cancelled'
 WHEN t.state='succeeded' AND NOT EXISTS(SELECT 1 FROM photo.v26_render_derivative_source(t.tenant_id,t.id)) THEN 'unavailable'
 WHEN r.status='completed' THEN CASE WHEN (
 SELECT count(*) FROM photo.tasks dt JOIN photo.artifacts a ON a.tenant_id=dt.tenant_id AND a.created_by_task_id=dt.id
 JOIN visible v ON v.artifact_id=a.id WHERE dt.tenant_id=r.tenant_id AND dt.run_id=r.id
 )=2 THEN 'ready' ELSE 'unavailable' END
 ELSE 'pending' END AS state
 FROM photo.tasks t JOIN photo.runs origin ON origin.tenant_id=t.tenant_id AND origin.id=t.run_id
 LEFT JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.v26_derivative_task_id=t.id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_source_run AND t.operation='render.compose' AND t.operation_version='1'
 ) SELECT count(*),count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='ready'),
 count(*) FILTER(WHERE state='failed'),count(*) FILTER(WHERE state='cancelled'),count(*) FILTER(WHERE state='unavailable') FROM frames;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_presentation_status(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_presentation_status(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_presentation_status(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
