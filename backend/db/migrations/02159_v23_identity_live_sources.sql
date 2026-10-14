-- +goose Up
-- Current project/version lineage for the two exact WorkingImage objects.
-- Called under the same transaction as V23 adoption; tenant lock serializes
-- deletion/revocation while source/version rows retain their object pins.
-- +goose StatementBegin
CREATE FUNCTION photo.v23_identity_live_sources(p_tenant uuid,p_task uuid,
 p_result uuid,p_before_version uuid,p_before uuid,p_before_sha text,
 p_before_key text,p_before_object_version text,p_after_version uuid,
 p_after uuid,p_after_sha text,p_after_key text,p_after_object_version text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,photo AS $$
DECLARE task_row photo.tasks%ROWTYPE;project uuid;source record;
BEGIN
 IF p_tenant IS NULL OR p_tenant IS DISTINCT FROM
  nullif(current_setting('app.tenant_id',true),'')::uuid OR
  p_task IS NULL OR p_result IS NULL OR p_before_version IS NULL OR
  p_after_version IS NULL OR p_before_version=p_after_version OR
  p_before IS NULL OR p_after IS NULL OR p_before=p_after OR
  p_before_sha !~ '^[0-9a-f]{64}$' OR
  p_after_sha !~ '^[0-9a-f]{64}$' OR
  coalesce(p_before_key,'')='' OR coalesce(p_after_key,'')='' OR
  coalesce(p_before_object_version,'') IN('','null') OR
  coalesce(p_after_object_version,'') IN('','null') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO task_row FROM photo.tasks t WHERE t.tenant_id=p_tenant AND
  t.id=p_task AND t.operation='identity.compare' AND
  t.operation_version='1' AND t.state='succeeded' AND
  NOT t.cancel_requested AND t.canonical_result_id=p_result AND
  t.attempt>0 AND t.fencing_token>0 FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT w.project_id INTO project FROM photo.v2_workflows w
  JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id
  JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=p_tenant AND w.run_id=task_row.run_id AND
   p.archived_at IS NULL AND NOT r.cancel_requested;
 IF project IS NULL OR NOT EXISTS(SELECT 1 FROM photo.task_results result
  WHERE result.tenant_id=p_tenant AND result.id=p_result AND
   result.task_id=p_task AND result.attempt=task_row.attempt AND
   result.fencing_token=task_row.fencing_token) THEN RETURN false;END IF;
 FOR source IN SELECT * FROM (VALUES
  (p_before_version,p_before,p_before_sha,p_before_key,p_before_object_version),
  (p_after_version,p_after,p_after_sha,p_after_key,p_after_object_version))
  AS chosen(version_id,artifact_id,sha256,object_key,object_version)
 LOOP
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.project_id=project AND i.task_id=p_task AND
   i.contract='WorkingImage' AND i.artifact_id=source.artifact_id AND
   i.sha256=source.sha256) THEN RETURN false;END IF;
  PERFORM version.id FROM photo.asset_versions version
   JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND
    asset.id=version.asset_id AND asset.project_id=project AND
    asset.deleted_at IS NULL AND asset.upload_state='verified'
   JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id AND
    original.id=asset.original_artifact_id AND original.state='ready' AND
    original.verified_at IS NOT NULL AND original.object_version IS NOT NULL AND
    original.object_version NOT IN('','null') AND
    (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
   JOIN photo.artifacts image ON image.tenant_id=version.tenant_id AND
    image.id=version.master_artifact_id AND image.sha256=version.content_hash AND
    image.id=source.artifact_id AND image.sha256=source.sha256 AND
    image.output_role='working_image' AND image.media_type='application/json' AND
    image.object_key=source.object_key AND image.object_version=source.object_version AND
    image.bytes BETWEEN 2 AND 1048576 AND image.state='ready' AND
    image.verified_at IS NOT NULL AND
    (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
   WHERE version.tenant_id=p_tenant AND version.id=source.version_id
   FOR SHARE OF version,asset,original,image;
  IF NOT FOUND THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
END $$;
ALTER FUNCTION photo.v23_identity_live_sources(uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_identity_live_sources(uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v23_identity_live_sources(uuid,uuid,uuid,uuid,uuid,text,text,text,uuid,uuid,text,text,text)
 TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
