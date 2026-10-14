-- +goose Up
-- Permit canonical original manual producer children before a V28 owner exists.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) RENAME TO v2_draft_artifact_scope_pre_v28_manual_children;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,
 p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid,p_parent uuid,p_parent_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.v2_draft_artifact_scope_pre_v28_manual_children(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session,p_parent,p_parent_sha) IS TRUE THEN RETURN true;END IF;
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_contract IS NULL OR p_contract NOT IN('MaskRef','MaskPixels','Artifact')
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(
 SELECT 1 FROM photo.artifacts item
 JOIN photo.tasks t ON t.tenant_id=item.tenant_id AND t.id=item.created_by_task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.interactive_tasks i ON i.tenant_id=t.tenant_id AND i.task_id=t.id
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id
  AND result.task_id=t.id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id
  AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.created_by_task_id=t.id
  AND root.output_role='rasterized_mask' AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 JOIN photo.asset_versions v ON v.tenant_id=t.tenant_id AND v.id=p_version AND v.master_artifact_id=i.source_artifact_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
 WHERE item.tenant_id=p_tenant AND item.id=p_artifact AND item.sha256=p_sha
  AND item.produced_attempt=t.attempt AND item.produced_fence=t.fencing_token
  AND t.operation='mask.rasterize' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
  AND r.kind='mask' AND r.graph_sealed AND NOT r.cancel_requested AND i.kind='mask_manual' AND i.operation=t.operation
  AND r.config_snapshot->>'asset_version_id'=p_version::text AND r.config_snapshot->>'source_sha256'=v.content_hash
  AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND v.review_state='accepted'
  AND source.output_role='working_image' AND source.media_type='application/json'
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),jsonb_build_object('artifact_id',item.id,'sha256',item.sha256))
  AND NOT EXISTS(SELECT 1 FROM (VALUES(source.state,source.verified_at,source.object_version,source.expires_at),
   (root.state,root.verified_at,root.object_version,root.expires_at),(item.state,item.verified_at,item.object_version,item.expires_at)) x(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
  AND ((p_contract='MaskRef' AND item.output_role IN('mask_target_ref','mask_guard_ref') AND item.media_type='application/json' AND item.bytes BETWEEN 2 AND 1048576 AND p_parent IS NULL AND p_parent_sha IS NULL)
   OR (p_contract='Artifact' AND item.output_role IN('mask_target_png','mask_protected_png') AND item.media_type='image/png' AND p_parent=root.id AND p_parent_sha=root.sha256)
   OR (p_contract='MaskPixels' AND item.output_role IN('mask_target','mask_protected') AND item.media_type='application/octet-stream' AND item.bytes BETWEEN 32 AND 800065548
    AND ((p_parent=root.id AND p_parent_sha=root.sha256) OR EXISTS(
     SELECT 1 FROM photo.artifacts doc WHERE doc.tenant_id=t.tenant_id AND doc.id=p_parent AND doc.sha256=p_parent_sha
      AND doc.created_by_task_id=t.id AND doc.produced_attempt=t.attempt AND doc.produced_fence=t.fencing_token
      AND doc.output_role=CASE item.output_role WHEN 'mask_target' THEN 'mask_target_ref' ELSE 'mask_guard_ref' END
      AND doc.state='ready' AND doc.verified_at IS NOT NULL AND doc.media_type='application/json'
      AND doc.object_version IS NOT NULL AND doc.object_version NOT IN('','null') AND (doc.expires_at IS NULL OR doc.expires_at>clock_timestamp())
      AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',doc.id,'sha256',doc.sha256)))))));
END $$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
