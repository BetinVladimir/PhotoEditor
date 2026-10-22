-- +goose Up
-- Authenticated read projection; no URLs, version mutations or new Apply.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_shown_applied_image(p_tenant uuid,p_target uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(child_run_id uuid,task_id uuid,result_id uuid,artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id,t.id,z.id,a.id,a.sha256 FROM photo.semantic_targets target
 JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
 JOIN photo.v2_workflows w ON w.tenant_id=intent.tenant_id AND w.id=intent.workflow_id AND w.project_id=target.project_id
 JOIN photo.v2_review_batches b ON b.tenant_id=target.tenant_id AND b.id=target.batch_id
 AND b.workflow_id=w.id AND b.project_id=w.project_id AND b.child_run_id=target.child_run_id AND b.action='apply'
 JOIN photo.runs r ON r.tenant_id=target.tenant_id AND r.id=target.child_run_id AND r.kind='apply'
 AND r.status='completed' AND r.graph_sealed AND NOT r.cancel_requested
 JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
 AND t.id=photo.semantic_uuid_v5(r.id,t.operation||':'||target.id::text)
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=(z.outputs->0->>'artifact_id')::uuid
 AND a.sha256=z.outputs->0->>'sha256' AND a.created_by_task_id=t.id
 AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token AND a.output_role='working_image'
 JOIN photo.tenants tenant ON tenant.id=target.tenant_id AND tenant.status='active'
 WHERE target.tenant_id=p_tenant AND target.id=p_target AND target.state='applied'
 AND t.operation IN('semantic.adopt_shown_exposure','semantic.adopt_shown_removal') AND t.operation_version='1' AND t.state='succeeded'
 AND NOT t.cancel_requested AND jsonb_typeof(z.outputs)='array' AND jsonb_array_length(z.outputs)=1
 AND r.config_snapshot->>'project_id'=target.project_id::text
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(r.config_snapshot->'shown_semantic'->'nodes') n
 WHERE n->>'task_id'=t.id::text AND n->>'target_id'=target.id::text AND n->>'params_artifact_id'=t.params_artifact_id::text)
 AND photo.v2_editor_authorized(p_tenant,target.project_id,p_actor,p_session)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
 AND i.event_id=photo.v2_adoption_event_id(t.id,z.id))
 AND EXISTS(SELECT 1 FROM photo.v2_artifact_links l WHERE l.tenant_id=a.tenant_id
 AND l.project_id=target.project_id AND l.workflow_id=w.id AND l.artifact_id=a.id AND l.sha256=a.sha256);
$$;
ALTER FUNCTION photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_shown_applied_image(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
