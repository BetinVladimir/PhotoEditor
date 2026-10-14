-- +goose Up
-- Accepted WorkingImage member of a sealed CorrectionPlan, with the exact
-- asset version fixed by the export snapshot. The caller checks the JSON edge.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_correction_member(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_source uuid,p_source_sha text,p_parent uuid,p_parent_sha text,p_version uuid,
 p_bucket text,p_source_key text,p_source_version text,p_source_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_source IS NULL OR p_parent IS NULL OR p_version IS NULL
  OR p_source=p_parent OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' OR coalesce(p_source_key,'')=''
  OR coalesce(p_source_version,'') IN ('','null')
  OR p_source_bytes<2 OR p_source_bytes>8388608 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_parent,p_parent_sha,'CorrectionPlan');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_source,p_source_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.id=p_version
  AND version.master_artifact_id=p_source AND version.content_hash=p_source_sha
  AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts source ON source.tenant_id=item.tenant_id
  AND source.id=p_source AND source.sha256=p_source_sha
  AND source.output_role='working_image' AND source.media_type='application/json'
 JOIN photo.artifacts parent ON parent.tenant_id=item.tenant_id
  AND parent.id=p_parent AND parent.sha256=p_parent_sha
  AND parent.media_type='application/json'
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_source AND item.sha256=p_source_sha
  AND source.bucket=p_bucket AND source.object_key=p_source_key
  AND source.object_version=p_source_version AND source.bytes=p_source_bytes
  AND source.state='ready' AND source.verified_at IS NOT NULL
  AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
  AND parent.state='ready' AND parent.verified_at IS NOT NULL
  AND parent.object_version IS NOT NULL AND parent.object_version NOT IN('','null')
  AND (parent.expires_at IS NULL OR parent.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_correction_member(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_correction_member(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_correction_member(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
