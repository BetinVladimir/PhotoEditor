-- +goose Up
-- Narrow authenticated object inventory; no general artifact read permission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_review_plan_sources(p_tenant uuid,p_workflow uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(run_id uuid,revision bigint,task_id uuid,result_id uuid,contract text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH assembly AS (
 SELECT r.id AS run_id,r.revision,t.id AS task_id,z.id AS result_id,w.project_id,
  (z.outputs->0->>'artifact_id')::uuid AS root_id,z.outputs->0->>'sha256' AS root_sha,
  t.attempt,t.fencing_token
 FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.node_key='v2.semantic.assemble:0' AND t.operation='v2.semantic.assemble' AND t.operation_version='1' AND t.resource_class='cpu'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.tenants tenant ON tenant.id=w.tenant_id
 WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND w.workflow_type='semantic' AND w.renderer_version='v2.2'
 AND r.kind='v2' AND r.status IN('waiting_review','completed') AND NOT r.cancel_requested AND r.graph_sealed
 AND tenant.status='active' AND w.input_snapshot_id=r.input_snapshot_id
 AND photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session)
 AND t.state='succeeded' AND NOT t.cancel_requested
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
  AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND EXISTS(SELECT 1 FROM photo.artifacts root WHERE root.tenant_id=p_tenant AND root.id=(z.outputs->0->>'artifact_id')::uuid
  AND root.sha256=z.outputs->0->>'sha256' AND root.state='ready' AND root.verified_at IS NOT NULL
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 8388608
  AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null')
  AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
  AND root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token)
 ), inventory AS (
 SELECT a.*, 'SemanticPlan'::text AS contract,a.root_id AS artifact_id,a.root_sha AS sha256 FROM assembly a
 UNION ALL
 SELECT a.*,declared.contract,declared.artifact_id,declared.sha256 FROM assembly a
 JOIN photo.v2_task_inputs declared ON declared.tenant_id=p_tenant AND declared.project_id=a.project_id AND declared.task_id=a.task_id
 WHERE declared.contract IN('Params','InputSnapshot','EditIntent','Calibration','TransferCandidate')
 )
 SELECT i.run_id,i.revision,i.task_id,i.result_id,i.contract,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM inventory i JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 8388608
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (i.contract<>'SemanticPlan' OR (a.created_by_task_id=i.task_id AND a.produced_attempt=i.attempt AND a.produced_fence=i.fencing_token))
 ORDER BY i.contract,a.id;
$$;
ALTER FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_review_plan_sources(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
