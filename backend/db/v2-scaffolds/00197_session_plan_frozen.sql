-- The plan compiler reads only admission-pinned configuration. A mutable
-- shoot_sessions row or current collection membership is never consulted.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_session_plan_frozen(p_tenant uuid,p_run uuid)
RETURNS TABLE(item_count integer,policy jsonb,policy_sha text,
 context_artifact_id uuid,context_sha text,snapshot_digest text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb; context_id uuid; context_digest text;
BEGIN
 SELECT r.config_snapshot->'v22_session_plan' INTO config FROM photo.runs r
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
  AND r.status='running' AND NOT r.cancel_requested AND NOT r.graph_sealed
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND w.input_snapshot_id=s.id AND w.project_id=(r.config_snapshot->>'project_id')::uuid
  AND s.kind='v2' AND s.principal_type='user' AND s.principal_id=r.created_by
  AND s.item_count BETWEEN 1 AND 5000 AND tenant.status='active'
  AND EXISTS(SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant
   AND e.run_id=p_run AND e.kind='v2.session.plan/1' AND e.state='open')
  AND jsonb_typeof(r.config_snapshot->'v22_session_plan')='object';
 IF config IS NULL OR (config-'schema_version'-'item_count'-'snapshot_digest'
  -'policy'-'context_sha')<>'{}'::jsonb
  OR config->>'schema_version'<>'1'
  OR coalesce(config->>'item_count','') !~ '^[1-9][0-9]{0,3}$'
  OR coalesce(config->>'snapshot_digest','') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(config->'policy')<>'object'
  OR coalesce(config->>'context_sha','') !~ '^[0-9a-f]{64}$'
  OR NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.snapshots s
   ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
   WHERE r.tenant_id=p_tenant AND r.id=p_run
    AND s.item_count=(config->>'item_count')::integer
    AND s.digest=config->>'snapshot_digest') THEN RETURN; END IF;
 SELECT d.artifact_id,d.sha256 INTO context_id,context_digest
 FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_run
  AND d.purpose='v2_analysis_context' AND d.state='ready'
  AND d.sha256=config->>'context_sha' AND a.sha256=d.sha256
  AND a.state='ready' AND a.media_type='application/json'
  AND a.verified_at IS NOT NULL AND a.object_version=d.object_version
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF context_id IS NULL THEN RETURN; END IF;
 RETURN QUERY SELECT (config->>'item_count')::integer,config->'policy',w.policy_sha,
  context_id,context_digest,config->>'snapshot_digest'
 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=p_run;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_session_plan_frozen(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_session_plan_frozen(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_session_plan_frozen(uuid,uuid) TO photo_scheduler;
