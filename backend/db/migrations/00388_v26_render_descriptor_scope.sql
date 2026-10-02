-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_descriptor_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_root uuid,p_root_sha text,p_descriptor uuid,p_descriptor_sha text,p_analysis uuid,p_ordinal integer,p_model_lock text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT coalesce(p_ordinal BETWEEN 0 AND 4999 AND photo.v26_render_recipe_scope(p_tenant,p_project,p_version,p_root,p_root_sha,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.session_analyses sa ON sa.tenant_id=root.tenant_id AND sa.project_id=root.project_id AND sa.id=p_analysis
 JOIN photo.v2_workflows w ON w.tenant_id=sa.tenant_id AND w.project_id=sa.project_id AND w.id=sa.workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.artifacts plan ON plan.tenant_id=sa.tenant_id AND plan.id=sa.plan_artifact_id
 JOIN photo.snapshot_items item ON item.tenant_id=sa.tenant_id AND item.snapshot_id=sa.snapshot_id
  AND item.asset_version_id=p_version AND item.ordinal=p_ordinal AND item.artifact_id=root.render_source_id AND item.sha256=root.render_source_sha
 JOIN photo.tasks task ON task.tenant_id=r.tenant_id AND task.run_id=r.id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.task_id=task.id AND result.id=task.canonical_result_id
  AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
 JOIN photo.artifacts descriptor ON descriptor.tenant_id=task.tenant_id AND descriptor.id=p_descriptor AND descriptor.sha256=p_descriptor_sha
 WHERE root.tenant_id=p_tenant AND root.project_id=p_project AND root.asset_version_id=p_version
 AND root.artifact_id=p_root AND root.sha256=p_root_sha AND root.state='ready' AND root.render_registered_at IS NOT NULL
 AND sa.model_lock_sha=p_model_lock AND w.workflow_type='session' AND w.renderer_version='v2.2' AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
 AND r.status IN('waiting_review','completed') AND plan.state='ready' AND plan.verified_at IS NOT NULL
 AND plan.object_version IS NOT NULL AND plan.object_version NOT IN('','null') AND (plan.expires_at IS NULL OR plan.expires_at>clock_timestamp())
 AND task.operation='session.describe' AND task.state='succeeded'
 AND jsonb_typeof(result.outputs)='array' AND jsonb_array_length(result.outputs)=1
 AND result.outputs->0->>'artifact_id'=p_descriptor::text AND result.outputs->0->>'sha256'=p_descriptor_sha
 AND descriptor.state='ready' AND descriptor.output_role='document' AND descriptor.media_type='application/json'
 AND descriptor.verified_at IS NOT NULL AND descriptor.object_version IS NOT NULL AND descriptor.object_version NOT IN('','null')
 AND (descriptor.expires_at IS NULL OR descriptor.expires_at>clock_timestamp())
 AND descriptor.created_by_task_id=task.id AND descriptor.produced_attempt=task.attempt AND descriptor.produced_fence=task.fencing_token
 AND EXISTS(SELECT 1 FROM photo.scene_groups g JOIN photo.scene_members member ON member.tenant_id=g.tenant_id AND member.project_id=g.project_id AND member.group_id=g.id
  WHERE g.tenant_id=sa.tenant_id AND g.project_id=sa.project_id AND g.analysis_id=sa.id AND member.asset_version_id=p_version AND member.snapshot_ordinal=p_ordinal)),false);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_descriptor_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_descriptor_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_descriptor_scope(uuid,uuid,uuid,uuid,text,uuid,text,uuid,integer,text,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
