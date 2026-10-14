-- +goose Up
-- Origin run API reads only completed, verified receipt-bound previews.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_render_derivative_outputs(p_user uuid,p_session uuid,p_tenant uuid,p_source_run uuid)
RETURNS TABLE(artifact_id uuid,output_role text,sha256 text,bytes bigint,media_type text,bucket text,object_key text,object_version text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE role_ref text;project_ref uuid;visibility_ref text;
BEGIN
 SELECT a.role INTO role_ref FROM photo.authorize_tenant(p_user,p_session,p_tenant) a;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT p.id,p.visibility INTO project_ref,visibility_ref FROM photo.projects p
 WHERE p.tenant_id=p_tenant AND p.archived_at IS NULL AND (
 EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p.tenant_id AND w.project_id=p.id AND w.run_id=p_source_run AND w.workflow_type='preview')
 OR EXISTS(SELECT 1 FROM photo.semantic_targets target WHERE target.tenant_id=p.tenant_id AND target.project_id=p.id AND target.child_run_id=p_source_run AND target.state='applied'));
 IF NOT FOUND THEN RETURN;END IF;
 IF role_ref NOT IN('owner','admin') AND visibility_ref<>'workspace' AND NOT EXISTS(
 SELECT 1 FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=project_ref AND user_id=p_user) THEN RETURN;END IF;
 RETURN QUERY SELECT a.id,a.output_role,a.sha256,a.bytes,a.media_type,a.bucket,a.object_key,a.object_version
 FROM photo.runs r JOIN photo.tasks origin ON origin.tenant_id=r.tenant_id AND origin.id=r.v26_derivative_task_id AND origin.run_id=p_source_run
 CROSS JOIN LATERAL photo.v26_render_derivative_source(r.tenant_id,origin.id) s
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state='succeeded' AND NOT t.cancel_requested
 JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id AND d.purpose='task_params'
 CROSS JOIN LATERAL jsonb_array_elements(d.v26_derivative_receipt->'objects') o
 JOIN photo.artifacts a ON a.tenant_id=r.tenant_id AND a.id::text=o->>'artifact_id' AND a.sha256=o->>'sha256'
 JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id AND u.task_id=t.id
 WHERE r.tenant_id=p_tenant AND r.kind='preview' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
 AND s.project_id=project_ref AND r.config_snapshot->>'source_result_id'=s.result_id::text
 AND d.v26_derivative_receipt->>'result_id'=t.canonical_result_id::text
 AND d.v26_derivative_receipt->>'attempt'=t.attempt::text AND d.v26_derivative_receipt->>'fence'=t.fencing_token::text
 AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND a.bucket=o->>'bucket' AND a.object_key=o->>'key' AND a.object_version=o->>'version'
 AND a.bytes=(o->>'bytes')::bigint AND a.media_type=o->>'media_type' AND a.output_role=o->>'role'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND u.state='ready' AND u.attempt=t.attempt AND u.fence=t.fencing_token AND u.role=a.output_role
 AND u.bucket=a.bucket AND u.object_key=a.object_key AND u.object_version=a.object_version AND u.sha256=a.sha256 AND u.bytes=a.bytes AND u.media_type=a.media_type
 AND a.output_role IN('derivative','derivative_manifest') ORDER BY t.node_key,a.output_role,a.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_derivative_outputs(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
