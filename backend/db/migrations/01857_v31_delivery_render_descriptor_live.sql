-- +goose Up
-- Session-free counterpart of the frozen render descriptor admission,
-- anchored to the delegated task's exact frozen learning and current model.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_descriptor_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_descriptor uuid,p_descriptor_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;
BEGIN
 IF p_descriptor_sha!~'^[0-9a-f]{64}$' THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.session_analyses sa ON sa.tenant_id=root.tenant_id
  AND sa.project_id=root.project_id
  AND sa.id=(root.render_learning->>'analysis_id')::uuid
 JOIN photo.v2_workflows w ON w.tenant_id=sa.tenant_id
  AND w.project_id=sa.project_id AND w.id=sa.workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 JOIN photo.artifacts plan ON plan.tenant_id=sa.tenant_id
  AND plan.id=sa.plan_artifact_id
 JOIN photo.snapshot_items item ON item.tenant_id=sa.tenant_id
  AND item.snapshot_id=sa.snapshot_id
  AND item.asset_version_id=root.asset_version_id
  AND item.ordinal=(root.render_learning->>'ordinal')::integer
  AND item.artifact_id=root.render_source_id
  AND item.sha256=root.render_source_sha
 JOIN photo.tasks task ON task.tenant_id=r.tenant_id AND task.run_id=r.id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id
  AND result.task_id=task.id AND result.id=task.canonical_result_id
  AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token
 JOIN photo.artifacts descriptor ON descriptor.tenant_id=task.tenant_id
  AND descriptor.id=p_descriptor AND descriptor.sha256=p_descriptor_sha
 WHERE root.tenant_id=(source->>'tenant_id')::uuid
  AND root.project_id=(source->>'project_id')::uuid
  AND root.artifact_id=(e->'params'->>'artifact_id')::uuid
  AND root.sha256=e->'params'->>'sha256'
  AND root.state='ready' AND root.render_registered_at IS NOT NULL
  AND root.render_learning_checked AND root.render_learning IS NOT NULL
  AND root.render_learning->'descriptor'->>'artifact_id'=p_descriptor::text
  AND root.render_learning->'descriptor'->>'sha256'=p_descriptor_sha
  AND (root.render_learning->>'ordinal')~'^(0|[1-9][0-9]{0,3})$'
  AND (root.render_learning->>'ordinal')::integer BETWEEN 0 AND 4999
  AND root.render_learning_model_lock~'^[0-9a-f]{64}$'
  AND sa.model_lock_sha=root.render_learning_model_lock
  AND w.workflow_type='session' AND w.renderer_version='v2.2'
  AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested
  AND r.status IN('waiting_review','completed')
  AND plan.state='ready' AND plan.verified_at IS NOT NULL
  AND plan.object_version NOT IN('','null')
  AND (plan.expires_at IS NULL OR plan.expires_at>clock_timestamp())
  AND task.operation='session.describe' AND task.state='succeeded'
  AND jsonb_typeof(result.outputs)='array'
  AND jsonb_array_length(result.outputs)=1
  AND result.outputs->0->>'artifact_id'=p_descriptor::text
  AND result.outputs->0->>'sha256'=p_descriptor_sha
  AND descriptor.state='ready' AND descriptor.output_role='document'
  AND descriptor.media_type='application/json'
  AND descriptor.verified_at IS NOT NULL
  AND descriptor.object_version NOT IN('','null')
  AND (descriptor.expires_at IS NULL OR descriptor.expires_at>clock_timestamp())
  AND descriptor.created_by_task_id=task.id
  AND descriptor.produced_attempt=task.attempt
  AND descriptor.produced_fence=task.fencing_token
  AND EXISTS(SELECT 1 FROM photo.scene_groups g
   JOIN photo.scene_members member ON member.tenant_id=g.tenant_id
    AND member.project_id=g.project_id AND member.group_id=g.id
   WHERE g.tenant_id=sa.tenant_id AND g.project_id=sa.project_id
    AND g.analysis_id=sa.id AND member.asset_version_id=root.asset_version_id
    AND member.snapshot_ordinal=(root.render_learning->>'ordinal')::integer)
  AND photo.v31_delivery_render_model_live(p_expansion,p_token,p_ordinal,p_preset,
   p_task,(root.render_learning->'model'->>'artifact_id')::uuid,
   root.render_learning->'model'->>'sha256'));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_descriptor_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_descriptor_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_descriptor_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
