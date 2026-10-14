-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.pending_v29_negative_reviews(p_after uuid DEFAULT NULL)
RETURNS TABLE(tenant_id uuid,task_id uuid,root_id uuid,candidate_id uuid,params_sha text,bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.id,root.id,c.id,p.sha256,p.bucket,p.object_key,p.object_version,p.bytes
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id
 JOIN photo.artifacts p ON p.tenant_id=t.tenant_id AND p.id=t.params_artifact_id
 JOIN photo.v2_task_inputs pi ON pi.tenant_id=t.tenant_id AND pi.task_id=t.id AND pi.project_id=w.project_id
 AND pi.artifact_id=p.id AND pi.sha256=p.sha256 AND pi.contract='Params'
 JOIN photo.v2_artifact_links rl ON rl.tenant_id=w.tenant_id AND rl.workflow_id=w.id AND rl.project_id=w.project_id
 AND rl.ordinal=0 AND rl.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$','UTF8')),'hex')
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.id=rl.artifact_id AND root.sha256=rl.sha256
 JOIN photo.v2_artifact_links cl ON cl.tenant_id=w.tenant_id AND cl.workflow_id=w.id AND cl.project_id=w.project_id
 AND cl.purpose=encode(sha256(convert_to('tasks/'||t.id||'/$/working_image','UTF8')),'hex')
 JOIN photo.artifacts c ON c.tenant_id=t.tenant_id AND c.id=cl.artifact_id AND c.sha256=cl.sha256
 WHERE t.operation='optical.negative' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND (p_after IS NULL OR t.id>p_after) AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND r.status IN('running','waiting_review') AND z.review_required AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND z.outputs=jsonb_build_array(jsonb_build_object('artifact_id',root.id::text,'sha256',root.sha256))
 AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 AND c.created_by_task_id=t.id AND c.produced_attempt=t.attempt AND c.produced_fence=t.fencing_token
 AND root.output_role='document' AND c.output_role='working_image'
 AND root.state='ready' AND root.verified_at IS NOT NULL AND root.media_type='application/json'
 AND c.state='ready' AND c.verified_at IS NOT NULL AND c.media_type='application/json'
 AND p.state='ready' AND p.verified_at IS NOT NULL AND p.media_type='application/json' AND p.bytes BETWEEN 2 AND 65536
 AND p.object_version NOT IN('','null') AND root.object_version NOT IN('','null') AND c.object_version NOT IN('','null')
 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())
 AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM photo.reviews v WHERE v.tenant_id=t.tenant_id AND v.task_id=t.id)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks pending WHERE pending.tenant_id=t.tenant_id AND pending.run_id=t.run_id
 AND (pending.state<>'succeeded' OR pending.canonical_result_id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.inbox i
 WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(pending.id,pending.canonical_result_id))))
 ORDER BY t.id LIMIT 100
$$;
ALTER FUNCTION photo.pending_v29_negative_reviews(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v29_negative_reviews(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pending_v29_negative_reviews(uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
