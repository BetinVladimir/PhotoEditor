-- +goose Up
-- Discover unadopted canonical identity results for the Go scheduler's
-- physical verifier. The result and Params are rechecked during commit.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v23_identity_adoption(p_after uuid DEFAULT NULL)
RETURNS TABLE(tenant_id uuid,task_id uuid,result_id uuid,root_id uuid,
 root_sha text,params_id uuid,params_sha text,attempt integer,fence bigint)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.id,z.id,root.id,root.sha256,
  params.id,params.sha256,t.attempt,t.fencing_token
 FROM photo.tasks t
 JOIN photo.tenants tenant ON tenant.id=t.tenant_id AND tenant.status='active'
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id AND
  r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 JOIN photo.v2_workflows w ON w.tenant_id=t.tenant_id AND w.run_id=t.run_id AND
  w.renderer_version='v2.2'
 JOIN photo.projects project ON project.tenant_id=t.tenant_id AND
  project.id=w.project_id AND project.archived_at IS NULL
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND
  z.id=t.canonical_result_id AND z.task_id=t.id AND
  z.attempt=t.attempt AND z.fencing_token=t.fencing_token AND
  jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND
  root.id::text=(z.outputs->0->>'artifact_id') AND
  root.sha256=(z.outputs->0->>'sha256') AND root.output_role='document' AND
  root.media_type='application/json' AND root.state='ready' AND
  root.created_by_task_id=t.id AND root.produced_attempt=t.attempt AND
  root.produced_fence=t.fencing_token AND root.verified_at IS NOT NULL AND
  root.object_version IS NOT NULL AND root.object_version NOT IN('','null') AND
  (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
 JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND
  i.project_id=w.project_id AND i.task_id=t.id AND i.contract='Params' AND
  i.artifact_id=t.params_artifact_id
 JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND
  params.id=i.artifact_id AND params.sha256=i.sha256 AND
  params.media_type='application/json' AND params.state='ready' AND
  params.verified_at IS NOT NULL AND params.object_version IS NOT NULL AND
  params.object_version NOT IN('','null') AND
  (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 WHERE (p_after IS NULL OR t.id>p_after) AND
  t.operation='identity.compare' AND t.operation_version='1' AND
  t.resource_class='gpu' AND t.state='succeeded' AND NOT t.cancel_requested AND
  t.canonical_result_id IS NOT NULL AND t.attempt>0 AND t.fencing_token>0 AND
  NOT EXISTS(SELECT 1 FROM photo.inbox adopted WHERE adopted.tenant_id=t.tenant_id
   AND adopted.consumer='v2-adopt' AND
   adopted.event_id=photo.v2_adoption_event_id(t.id,z.id))
 ORDER BY t.id LIMIT 1;
$$;
ALTER FUNCTION photo.next_v23_identity_adoption(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v23_identity_adoption(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.next_v23_identity_adoption(uuid) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
