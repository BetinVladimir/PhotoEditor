-- +goose Up
-- Canonical Apply exposure input/output evidence; no general Apply artifact access.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_exposure_adoption_artifact(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,
 media_type text,role text,source text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
  CASE WHEN u.id IS NOT NULL THEN u.role ELSE NULL END,
  CASE WHEN u.id IS NOT NULL THEN 'output' ELSE 'input' END
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 CROSS JOIN LATERAL photo.v27_exposure_adoption_context(t.tenant_id,t.id) scope
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 LEFT JOIN photo.output_uploads u ON u.tenant_id=t.tenant_id AND u.artifact_id=a.id
  AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.state='ready'
 LEFT JOIN photo.v2_task_inputs i ON i.tenant_id=t.tenant_id AND i.project_id=(r.config_snapshot->>'project_id')::uuid
  AND i.task_id=t.id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.attempt=p_attempt AND t.fencing_token=p_fence
 AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL AND NOT t.cancel_requested
 AND r.kind='apply' AND r.status='running' AND NOT r.cancel_requested AND tenant.status='active'
 AND t.execution_profile->>'renderer_version'='v2.2'
 AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (u.id IS NOT NULL OR i.id IS NOT NULL)
 AND (u.id IS NULL OR (a.created_by_task_id=t.id AND a.produced_attempt=p_attempt
  AND a.produced_fence=p_fence AND a.sha256=u.sha256 AND a.output_role=u.role));
$$;

ALTER FUNCTION photo.v27_exposure_adoption_artifact(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_exposure_adoption_artifact(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_exposure_adoption_artifact(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
CREATE FUNCTION photo.v27_exposure_adoption_staged_outputs(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint)
RETURNS TABLE(artifact_id uuid,sha256 text,state text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT u.artifact_id,u.sha256,u.state FROM photo.output_uploads u
 JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 CROSS JOIN LATERAL photo.v27_exposure_adoption_context(t.tenant_id,t.id) scope
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE u.tenant_id=p_tenant AND u.task_id=p_task AND u.attempt=p_attempt AND u.fence=p_fence
 AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.state='succeeded'
 AND NOT t.cancel_requested AND r.kind='apply' AND r.status='running' AND NOT r.cancel_requested
 AND tenant.status='active'
 AND t.execution_profile->>'renderer_version'='v2.2';
$$;

ALTER FUNCTION photo.v27_exposure_adoption_staged_outputs(uuid,uuid,integer,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_exposure_adoption_staged_outputs(uuid,uuid,integer,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_exposure_adoption_staged_outputs(uuid,uuid,integer,bigint) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
