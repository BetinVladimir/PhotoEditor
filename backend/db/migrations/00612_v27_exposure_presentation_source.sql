-- +goose Up
-- Private presentation preparation metadata; no public artifact/URL access.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_exposure_presentation_source(p_tenant uuid,p_target uuid)
RETURNS TABLE(project_id uuid,child_run_id uuid,task_id uuid,result_id uuid,attempt integer,fence bigint,
 root_artifact_id uuid,root_sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT target.project_id,r.id,t.id,z.id,t.attempt,t.fencing_token,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.semantic_targets target
 JOIN photo.runs r ON r.tenant_id=target.tenant_id AND r.id=target.child_run_id
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.id=photo.semantic_uuid_v5(r.id,'semantic.edit:'||target.id::text)
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid AND a.sha256=z.outputs->0->>'sha256'
 JOIN photo.projects p ON p.tenant_id=target.tenant_id AND p.id=target.project_id AND p.archived_at IS NULL
 JOIN photo.tenants tenant ON tenant.id=target.tenant_id AND tenant.status='active'
 WHERE target.tenant_id=p_tenant AND target.id=p_target AND target.state='applied'
 AND r.kind='apply' AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested AND r.config_snapshot ? 'approved_semantic'
 AND t.state='succeeded' AND NOT t.cancel_requested AND t.operation='semantic.local_exposure' AND t.operation_version='1'
 AND jsonb_array_length(z.outputs)=1 AND a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
 AND a.output_role='document' AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,z.id));
$$;
ALTER FUNCTION photo.semantic_exposure_presentation_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_exposure_presentation_source(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.semantic_exposure_presentation_source(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
