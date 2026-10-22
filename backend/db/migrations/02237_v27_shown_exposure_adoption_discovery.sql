-- +goose Up
-- Discover only canonical exposure results bound to their committed review child.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_shown_exposure_adoption_context(p_tenant uuid,p_task uuid)
RETURNS TABLE(attempt integer,fence bigint,result_id uuid,operation text,operation_version text,
 root_artifact_id uuid,root_sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.attempt,t.fencing_token,z.id,t.operation,t.operation_version,
  (z.outputs->0->>'artifact_id')::uuid,z.outputs->0->>'sha256'
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.state='succeeded'
 AND r.kind='apply' AND r.status='running' AND NOT r.cancel_requested AND NOT t.cancel_requested
 AND tenant.status='active'
 AND t.operation='semantic.adopt_shown_exposure' AND t.operation_version='1' AND t.resource_class='cpu'
 AND EXISTS(SELECT 1 FROM photo.v27_shown_exposure_adoption_params(p_tenant,p_task,t.attempt,t.fencing_token,z.id))
 AND t.execution_profile->>'renderer_version'='v2.2'
 AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token;
$$;

ALTER FUNCTION photo.v27_shown_exposure_adoption_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_shown_exposure_adoption_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_shown_exposure_adoption_context(uuid,uuid) TO photo_scheduler;
CREATE FUNCTION photo.v27_pending_shown_exposure_adoptions() RETURNS TABLE(tenant_id uuid,run_id uuid,task_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT t.tenant_id,t.run_id,t.id FROM photo.tasks t
 CROSS JOIN LATERAL photo.v27_shown_exposure_adoption_context(t.tenant_id,t.id) c
 WHERE t.operation='semantic.adopt_shown_exposure' AND t.operation_version='1'
 AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id
 AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,c.result_id))
 ORDER BY t.created_at,t.id LIMIT 64;
$$;
ALTER FUNCTION photo.v27_pending_shown_exposure_adoptions() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_pending_shown_exposure_adoptions() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_pending_shown_exposure_adoptions() TO photo_scheduler;
-- +goose StatementEnd
