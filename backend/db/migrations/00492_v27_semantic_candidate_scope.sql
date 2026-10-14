-- +goose Up
-- Read-only scheduler evidence gate; document semantics remain worker verified.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_candidate_scope(p_tenant uuid,p_workflow uuid,p_candidate uuid,p_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_sha ~ '^[0-9a-f]{64}$' AND EXISTS(
  SELECT 1 FROM photo.v2_workflows w
  JOIN photo.tenants tenant ON tenant.id=w.tenant_id
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id
   AND z.id=t.canonical_result_id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_candidate AND a.sha256=p_sha
  WHERE w.tenant_id=p_tenant AND w.id=p_workflow AND w.workflow_type='semantic'
   AND w.renderer_version='v2.2' AND tenant.status='active'
   AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
   AND r.status IN('running','waiting_review','completed')
   AND t.operation='semantic.transfer' AND t.operation_version='1' AND t.resource_class='gpu'
   AND t.state='succeeded' AND NOT t.cancel_requested AND t.attempt>0 AND t.fencing_token>0
   AND a.state='ready' AND a.output_role='document' AND a.media_type='application/json'
   AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
   AND jsonb_typeof(z.outputs)='array'
   AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_candidate::text,'sha256',p_sha))
   AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
    AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 ),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v27_semantic_candidate_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_candidate_scope(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_candidate_scope(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
