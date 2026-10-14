-- +goose Up
-- Extend draft preview metadata to exact accepted Apply evidence.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_derivative_artifact(p_tenant uuid,p_run uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.runs r
 CROSS JOIN LATERAL photo.v26_render_derivative_source(r.tenant_id,r.v26_derivative_task_id) s
 JOIN photo.artifacts a ON a.tenant_id=r.tenant_id AND a.id=p_artifact AND a.sha256=p_sha
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.v26_derivative_task_id IS NOT NULL
 AND r.kind='preview' AND ((r.status='draft' AND NOT r.graph_sealed) OR (r.status='running' AND r.graph_sealed)) AND NOT r.cancel_requested
 AND r.config_snapshot->>'source_result_id'=s.result_id::text
 AND r.config_snapshot->>'source_task_id'=r.v26_derivative_task_id::text
 AND r.config_snapshot->>'source_run_id'=s.run_id::text
 AND r.config_snapshot->>'project_id'=s.project_id::text
 AND r.config_snapshot->>'source_attempt'=s.attempt::text AND r.config_snapshot->>'source_fence'=s.fence::text
 AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
 AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (a.id=s.root_id AND a.sha256=s.root_sha OR EXISTS(
  SELECT 1 FROM photo.v2_workflows w JOIN photo.v2_artifact_links l
  ON l.tenant_id=w.tenant_id AND l.workflow_id=w.id
  WHERE w.tenant_id=p_tenant AND w.run_id=s.run_id AND l.artifact_id=a.id AND l.sha256=a.sha256)
 OR EXISTS(SELECT 1 FROM photo.semantic_targets target
  WHERE target.tenant_id=p_tenant AND target.child_run_id=s.run_id
  AND (EXISTS(SELECT 1 FROM photo.semantic_exposure_presentation_evidence(p_tenant,target.id,a.id) e WHERE e.sha256=a.sha256)
   OR EXISTS(SELECT 1 FROM photo.semantic_exposure_presentation_input(p_tenant,target.id,a.id) i WHERE i.sha256=a.sha256))));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_derivative_artifact(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_artifact(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_derivative_artifact(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
