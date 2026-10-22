-- +goose Up
-- +goose StatementBegin
-- Local canonical image ancestry; archive IDs and matching geometry grant none.
CREATE FUNCTION photo.v30_canonical_image_ancestor(p_tenant uuid,p_project uuid,p_current uuid,p_current_sha text,p_ancestor uuid,p_ancestor_sha text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH RECURSIVE root AS (
 SELECT a.id,a.sha256 FROM photo.artifacts a
 JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.master_artifact_id=a.id AND v.content_hash=a.sha256 AND v.review_state='accepted'
 JOIN photo.assets owner ON owner.tenant_id=v.tenant_id AND owner.id=v.asset_id AND owner.project_id=p_project AND owner.deleted_at IS NULL AND owner.upload_state='verified'
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND a.tenant_id=p_tenant AND a.id=p_current AND a.sha256=p_current_sha AND a.output_role='working_image'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>statement_timestamp())
 ), chain(id,sha256,depth) AS (
 SELECT id,sha256,0 FROM root
 UNION
 SELECT input.id,input.sha256,c.depth+1 FROM chain c
 JOIN photo.artifacts output ON output.tenant_id=p_tenant AND output.id=c.id AND output.sha256=c.sha256
 JOIN photo.tasks task ON task.tenant_id=output.tenant_id AND task.id=output.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id AND result.task_id=task.id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=task.tenant_id AND attempt.task_id=task.id AND attempt.attempt=task.attempt AND attempt.fencing_token=task.fencing_token
 JOIN photo.runs run ON run.tenant_id=task.tenant_id AND run.id=task.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
 CROSS JOIN LATERAL jsonb_array_elements(task.inputs) declared
 JOIN photo.artifacts input ON input.tenant_id=task.tenant_id AND input.id=(declared->>'artifact_id')::uuid AND input.sha256=declared->>'sha256'
 WHERE c.depth<128
 AND task.state='succeeded' AND NOT task.cancel_requested AND NOT run.cancel_requested
 AND run.status IN('completed','completed_with_errors')
 AND (workflow.project_id=p_project OR run.config_snapshot->>'project_id'=p_project::text)
 AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token AND attempt.finished_at IS NOT NULL
 AND output.produced_attempt=result.attempt AND output.produced_fence=result.fencing_token
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',output.id,'sha256',output.sha256))
 AND input.output_role='working_image' AND input.state='ready' AND input.verified_at IS NOT NULL AND input.object_version NOT IN('','null')
 AND (input.expires_at IS NULL OR input.expires_at>statement_timestamp())
 ) SELECT EXISTS(SELECT FROM chain WHERE id=p_ancestor AND sha256=p_ancestor_sha)
$$;
ALTER FUNCTION photo.v30_canonical_image_ancestor(uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_canonical_image_ancestor(uuid,uuid,uuid,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_canonical_image_ancestor(uuid,uuid,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
