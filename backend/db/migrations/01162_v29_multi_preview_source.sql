-- +goose Up
-- Scheduler-only current observation; not a dispatch lease or preview admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_preview_source(p_tenant uuid,p_task uuid,p_operation text)
RETURNS TABLE(project_id uuid,run_id uuid,workflow_id uuid,result_id uuid,
 root_id uuid,root_sha text,snapshot_id uuid,params_jcs text,
 attempt integer,fence bigint,actor_id uuid,auth_session_id uuid)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.project_id,r.id,w.id,b.result_id,b.artifact_id,b.sha256,b.snapshot_id,b.params_jcs,
 t.attempt,t.fencing_token,(plan.body->>'actor_id')::uuid,(plan.body->>'auth_session_id')::uuid
 FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 CROSS JOIN LATERAL (SELECT r.config_snapshot->'v29_multi_plan' AS body) plan
 CROSS JOIN LATERAL photo.v29_adopted_multi_binding(p_tenant,w.project_id,p_task,
  (plan.body->>'actor_id')::uuid,(plan.body->>'auth_session_id')::uuid,p_operation) b
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND t.tenant_id=p_tenant AND t.id=p_task
 AND p_operation IN('optical.panorama','optical.focus_stack') AND t.operation=p_operation
 AND r.status IN('waiting_review','completed') AND NOT r.cancel_requested
 AND r.kind='v2' AND r.graph_sealed AND t.state='succeeded'
 AND plan.body->>'tenant_id'=p_tenant::text AND plan.body->>'project_id'=w.project_id::text
 AND plan.body->>'run_id'=r.id::text AND plan.body->>'workflow_id'=w.id::text
 AND plan.body->>'task_id'=t.id::text AND plan.body->>'snapshot_id'=r.input_snapshot_id::text
 AND b.run_id=r.id AND b.workflow_id=w.id AND b.snapshot_id=r.input_snapshot_id
 AND t.attempt>=1 AND t.fencing_token>=1;
$$;
ALTER FUNCTION photo.v29_multi_preview_source(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_preview_source(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_multi_preview_source(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
