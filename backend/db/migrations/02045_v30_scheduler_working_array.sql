-- +goose Up
-- Current source and same-producer authority for WorkingImage pixel/alpha
-- children, scoped to a leased canonical export and its frozen snapshot.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_working_array(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_parent uuid,p_parent_sha text,p_array uuid,p_array_sha text,p_contract text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_parent IS NULL OR p_array IS NULL OR p_parent=p_array
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_array_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_contract NOT IN('WorkingPixels','WorkingAlpha') THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_parent,p_parent_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_array,p_array_sha,p_contract);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_parent
  AND version.content_hash=p_parent_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts source ON source.tenant_id=version.tenant_id AND source.id=p_parent
  AND source.sha256=p_parent_sha AND source.output_role='working_image'
 JOIN photo.artifacts working_array ON working_array.tenant_id=source.tenant_id AND working_array.id=p_array
  AND working_array.sha256=p_array_sha AND working_array.output_role=CASE p_contract
   WHEN 'WorkingPixels' THEN 'working_rgb' ELSE 'alpha' END
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_parent AND item.sha256=p_parent_sha
  AND source.state='ready' AND source.verified_at IS NOT NULL
  AND working_array.state='ready' AND working_array.verified_at IS NOT NULL
  AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
  AND working_array.object_version IS NOT NULL AND working_array.object_version NOT IN('','null')
  AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
  AND (working_array.expires_at IS NULL OR working_array.expires_at>clock_timestamp())
  AND source.created_by_task_id=working_array.created_by_task_id
  AND working_array.created_by_task_id IS NOT NULL
  AND source.produced_attempt=working_array.produced_attempt
  AND working_array.produced_attempt IS NOT NULL
  AND source.produced_fence=working_array.produced_fence
  AND working_array.produced_fence IS NOT NULL);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_working_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_working_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_working_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
