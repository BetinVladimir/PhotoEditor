-- A pending replay may resume publication only for the exact admitted shell.
-- Compiler drift under an unchanged request hash is a conflict, not a chance
-- to publish a different graph into the existing draft run.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_pending_shell_matches(
 p_tenant uuid,p_run uuid,p_workflow uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_type text,p_policy_sha text,p_config_sha text,p_config jsonb,
 p_tasks jsonb,p_edges jsonb,p_bytes bigint) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_typeof(p_tasks)='array' AND jsonb_typeof(p_edges)='array'
 AND EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
   AND r.input_snapshot_id=p_snapshot AND r.created_by=p_actor
   AND r.config_hash=p_config_sha AND r.config_snapshot=p_config
   AND w.id=p_workflow AND w.project_id=p_project
   AND w.workflow_type=p_type AND w.policy_sha=p_policy_sha)
 AND EXISTS(SELECT 1 FROM photo.quota_reservations q
  WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
   AND q.state='reserved' AND q.bytes=p_bytes)
 AND coalesce((SELECT jsonb_agg(value ORDER BY value->>'id')
  FROM jsonb_array_elements(p_tasks)), '[]'::jsonb) =
  coalesce((SELECT jsonb_agg(jsonb_build_object(
   'id',t.id,'node_key',t.node_key,'operation',t.operation,
   'operation_version',t.operation_version,'resource_class',t.resource_class,
   'params_artifact_id',t.params_artifact_id,'params_sha',a.sha256,
   'input_digest',t.input_digest,'execution_profile',t.execution_profile)
   ORDER BY t.id::text)
   FROM photo.tasks t JOIN photo.artifacts a
    ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id
   WHERE t.tenant_id=p_tenant AND t.run_id=p_run), '[]'::jsonb)
 AND coalesce((SELECT jsonb_agg(value ORDER BY value->>'prerequisite_id',
   value->>'dependent_id') FROM jsonb_array_elements(p_edges)), '[]'::jsonb) =
  coalesce((SELECT jsonb_agg(jsonb_build_object(
   'prerequisite_id',e.prerequisite_id,'dependent_id',e.dependent_id)
   ORDER BY e.prerequisite_id::text,e.dependent_id::text)
   FROM photo.task_edges e WHERE e.tenant_id=p_tenant AND e.run_id=p_run
    AND e.condition='success'), '[]'::jsonb)
 AND NOT EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=p_tenant
  AND e.run_id=p_run AND e.condition<>'success')
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)
 FROM PUBLIC;
