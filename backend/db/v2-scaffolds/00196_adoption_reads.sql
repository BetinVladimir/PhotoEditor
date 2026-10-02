-- Read-only evidence for the trusted adopter. The object-store location is
-- returned only for an exact V2 task input or its own verified output.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_adoption_artifact(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,
 media_type text,role text,source text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
  CASE WHEN u.id IS NOT NULL THEN u.role ELSE NULL END,
  CASE WHEN u.id IS NOT NULL THEN 'output' ELSE 'input' END
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 LEFT JOIN photo.output_uploads u ON u.tenant_id=t.tenant_id AND u.artifact_id=a.id
  AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
 LEFT JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.project_id=w.project_id
  AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.attempt=p_attempt AND t.fencing_token=p_fence
 AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL AND NOT t.cancel_requested
 AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested AND tenant.status='active'
 AND w.renderer_version='v2.2' AND t.execution_profile->>'renderer_version'='v2.2'
 AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (u.id IS NOT NULL OR i.id IS NOT NULL)
 AND (u.id IS NULL OR (a.created_by_task_id=t.id AND a.produced_attempt=p_attempt
  AND a.produced_fence=p_fence AND a.sha256=u.sha256 AND a.output_role=u.role));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_adoption_artifact(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_adoption_artifact(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_adoption_artifact(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v2_adoption_staged_outputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint)
RETURNS TABLE(artifact_id uuid,sha256 text,state text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.artifact_id,u.sha256,u.state FROM photo.output_uploads u
 JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.state='succeeded'
 AND NOT t.cancel_requested AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested
 AND tenant.status='active' AND w.renderer_version='v2.2'
 AND t.execution_profile->>'renderer_version'='v2.2';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_adoption_staged_outputs(uuid,uuid,integer,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_adoption_staged_outputs(uuid,uuid,integer,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_adoption_staged_outputs(uuid,uuid,integer,bigint) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v2_adoption_context(p_tenant uuid,p_task uuid)
RETURNS TABLE(attempt integer,fence bigint,result_id uuid,operation text,operation_version text,
 root_artifact_id uuid,root_sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.attempt,t.fencing_token,z.id,t.operation,t.operation_version,
  (z.outputs->0->>'artifact_id')::uuid,z.outputs->0->>'sha256'
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.state='succeeded'
 AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested AND NOT t.cancel_requested
 AND tenant.status='active' AND w.renderer_version='v2.2'
 AND t.execution_profile->>'renderer_version'='v2.2'
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_adoption_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_adoption_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_adoption_context(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v2_pending_adoptions() RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id FROM photo.tasks t
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE r.kind='v2' AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND tenant.status='active' AND t.state='succeeded' AND NOT t.cancel_requested
 AND t.canonical_result_id IS NOT NULL AND t.execution_profile->>'renderer_version'='v2.2'
 AND w.renderer_version='v2.2'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
  AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 ORDER BY t.created_at,t.id LIMIT 64;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_pending_adoptions() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_adoptions() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_pending_adoptions() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v2_pending_finalizations() RETURNS TABLE(tenant_id uuid,run_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id FROM photo.runs r JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE r.kind='v2' AND r.status IN ('running','cancelling') AND r.graph_sealed
 AND tenant.status='active'
 AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
  AND t.state NOT IN ('succeeded','failed','cancelled','skipped'))
 AND (r.cancel_requested OR r.status='cancelling' OR NOT EXISTS(
  SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=r.tenant_id
   AND e.run_id=r.id AND e.state<>'closed'))
 AND (r.cancel_requested OR r.status='cancelling'
  OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
   AND t.state IN ('failed','cancelled','skipped'))
  OR NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
   AND (t.state<>'succeeded' OR t.canonical_result_id IS NULL
    OR NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
     AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)))))
 ORDER BY r.created_at,r.id LIMIT 64;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_pending_finalizations() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_finalizations() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_pending_finalizations() TO photo_scheduler;
