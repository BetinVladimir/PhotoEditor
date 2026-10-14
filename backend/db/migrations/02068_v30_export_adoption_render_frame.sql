-- +goose Up
-- A frame is eligible only through its registered learning descriptor and
-- the exact session analysis snapshot. The caller validates JSON child edges.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_render_frame(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_root uuid,p_root_sha text,p_descriptor uuid,p_descriptor_sha text,
 p_selected_version uuid,p_learning jsonb,p_lock text,p_ordinal integer,
 p_frame_version uuid,p_source uuid,p_source_sha text,
 p_contract text,p_child uuid,p_child_sha text,
 p_bucket text,p_object_key text,p_object_version text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;draft photo.v2_draft_uploads%ROWTYPE;
 recipe photo.artifacts%ROWTYPE;descriptor photo.artifacts%ROWTYPE;
BEGIN
 IF p_root IS NULL OR p_descriptor IS NULL OR p_source IS NULL OR p_child IS NULL
  OR p_selected_version IS NULL OR p_frame_version IS NULL
  OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_descriptor_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_child_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_contract IS NULL OR p_contract NOT IN ('WorkingImage','FaceObservations')
  OR p_ordinal NOT BETWEEN 0 AND 4999
  OR coalesce(p_lock,'') !~ '^[0-9a-f]{64}$'
  OR p_learning#>>'{descriptor,artifact_id}' IS DISTINCT FROM p_descriptor::text
  OR p_learning#>>'{descriptor,sha256}' IS DISTINCT FROM p_descriptor_sha
  OR coalesce(p_bucket,'')='' OR coalesce(p_object_key,'')=''
  OR coalesce(p_object_version,'') IN ('','null')
  OR p_bytes NOT BETWEEN 2 AND 8388608
  OR (p_contract='WorkingImage' AND (p_child<>p_source OR p_child_sha<>p_source_sha))
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_root,p_root_sha,'FrozenRenderRecipe');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_descriptor,p_descriptor_sha,'DescriptorShard');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_source,p_source_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 IF p_contract='FaceObservations' THEN
  PERFORM 1 FROM photo.v30_export_adoption_result_input(
   p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
   p_child,p_child_sha,'FaceObservations');
  IF NOT FOUND THEN RETURN false;END IF;
 END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 SELECT * INTO draft FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant
  AND project_id=w.project_id AND artifact_id=p_root AND sha256=p_root_sha
  AND asset_version_id=p_selected_version AND state='ready'
  AND render_registered_at IS NOT NULL FOR SHARE;
 SELECT * INTO recipe FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_root
  AND sha256=p_root_sha FOR SHARE;
 SELECT * INTO descriptor FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=p_descriptor AND sha256=p_descriptor_sha FOR SHARE;
 IF w.id IS NULL OR draft.artifact_id IS NULL OR recipe.id IS NULL OR descriptor.id IS NULL
  OR photo.v30_export_adoption_learning_occurrence(
   p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
   p_root,p_root_sha,p_selected_version,draft.render_source_id,
   draft.render_source_sha,p_learning,p_lock,'DescriptorShard',
   p_descriptor,p_descriptor_sha,descriptor.bucket,descriptor.object_key,
   descriptor.object_version,descriptor.bytes,recipe.object_version) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.session_analyses analysis
  JOIN photo.snapshot_items item ON item.tenant_id=analysis.tenant_id
   AND item.snapshot_id=analysis.snapshot_id AND item.ordinal=p_ordinal
   AND item.asset_version_id=p_frame_version AND item.artifact_id=p_source
   AND item.sha256=p_source_sha
  JOIN photo.snapshot_items frozen_item ON frozen_item.tenant_id=item.tenant_id
   AND frozen_item.snapshot_id=w.input_snapshot_id
   AND frozen_item.asset_version_id=item.asset_version_id
   AND frozen_item.artifact_id=item.artifact_id AND frozen_item.sha256=item.sha256
  JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
   AND version.id=item.asset_version_id AND version.master_artifact_id=p_source
   AND version.content_hash=p_source_sha AND version.review_state='accepted'
  JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
   AND asset.id=version.asset_id AND asset.project_id=analysis.project_id
   AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  JOIN photo.artifacts source ON source.tenant_id=item.tenant_id
   AND source.id=p_source AND source.sha256=p_source_sha
  WHERE analysis.tenant_id=p_tenant AND analysis.project_id=w.project_id
   AND analysis.id=(p_learning->>'analysis_id')::uuid
   AND analysis.model_lock_sha=p_lock
   AND source.state='ready' AND source.verified_at IS NOT NULL
   AND source.output_role='working_image' AND source.media_type='application/json'
   AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
   AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.scene_groups g
    JOIN photo.scene_members member ON member.tenant_id=g.tenant_id
     AND member.project_id=g.project_id AND member.group_id=g.id
    WHERE g.tenant_id=analysis.tenant_id AND g.project_id=analysis.project_id
     AND g.analysis_id=analysis.id AND member.asset_version_id=p_frame_version
     AND member.snapshot_ordinal=p_ordinal)) THEN RETURN false;END IF;
 IF p_contract='WorkingImage' THEN
  RETURN EXISTS(SELECT 1 FROM photo.artifacts source WHERE source.tenant_id=p_tenant
   AND source.id=p_source AND source.sha256=p_source_sha
   AND source.bucket=p_bucket AND source.object_key=p_object_key
   AND source.object_version=p_object_version AND source.bytes=p_bytes);
 END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts faces
  JOIN photo.output_uploads upload ON upload.tenant_id=faces.tenant_id
   AND upload.artifact_id=faces.id AND upload.task_id=faces.created_by_task_id
   AND upload.attempt=faces.produced_attempt AND upload.fence=faces.produced_fence
  WHERE faces.tenant_id=p_tenant AND faces.id=p_child AND faces.sha256=p_child_sha
   AND faces.bucket=p_bucket AND faces.object_key=p_object_key
   AND faces.object_version=p_object_version AND faces.bytes=p_bytes
   AND faces.created_by_task_id=descriptor.created_by_task_id
   AND faces.produced_attempt=descriptor.produced_attempt
   AND faces.produced_fence=descriptor.produced_fence
   AND faces.output_role='faces' AND faces.media_type='application/json'
   AND faces.state='ready' AND faces.verified_at IS NOT NULL
   AND (faces.expires_at IS NULL OR faces.expires_at>clock_timestamp())
   AND upload.state='ready' AND upload.role='faces'
   AND upload.sha256=faces.sha256 AND upload.object_version=faces.object_version
   AND upload.bytes=faces.bytes);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_render_frame(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_render_frame(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_render_frame(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
