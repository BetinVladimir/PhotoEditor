-- +goose Up
-- Private canonical landmark provenance; content/geometry remain separate gates.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_landmark_origin(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_source jsonb,p_landmark uuid,p_sha text)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR jsonb_typeof(p_source) IS DISTINCT FROM 'object'
 OR p_landmark IS NULL OR p_landmark='00000000-0000-0000-0000-000000000000'::uuid
 OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 RETURN EXISTS(
 SELECT 1 FROM photo.artifacts a
 JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 LEFT JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.task_id=t.id
 AND result.id=t.canonical_result_id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=t.tenant_id AND attempt.task_id=t.id
 AND attempt.attempt=t.attempt AND attempt.fencing_token=t.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.output_uploads upload ON upload.tenant_id=a.tenant_id AND upload.artifact_id=a.id
 AND upload.task_id=t.id AND upload.attempt=t.attempt AND upload.fence=t.fencing_token
 JOIN photo.asset_versions version ON version.tenant_id=a.tenant_id AND version.id=(p_source->>'asset_version_id')::uuid
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 JOIN photo.projects project ON project.tenant_id=asset.tenant_id AND project.id=asset.project_id
 JOIN photo.tenants tenant ON tenant.id=asset.tenant_id
 JOIN photo.artifacts source ON source.tenant_id=version.tenant_id AND source.id=version.master_artifact_id
 WHERE a.tenant_id=p_tenant AND a.id=p_landmark AND a.sha256=p_sha
 AND a.output_role='face_landmarks' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND t.operation='face.landmarks' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND a.produced_attempt=t.attempt AND a.produced_fence=t.fencing_token
 AND attempt.backend IN('cpu','cuda','mps') AND t.execution_profile->>'backend'=attempt.backend
 AND attempt.profile->>'backend'=attempt.backend
 AND NOT r.cancel_requested AND (w.project_id=p_project OR r.config_snapshot->>'project_id'=p_project::text)
 AND jsonb_typeof(result.outputs)='array'
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
 AND upload.state='ready' AND upload.completed_at IS NOT NULL AND upload.role=a.output_role
 AND upload.bytes=a.bytes AND upload.sha256=a.sha256 AND upload.bucket=a.bucket
 AND upload.object_key=a.object_key AND upload.object_version=a.object_version AND upload.media_type=a.media_type
 AND tenant.status='active' AND project.id=p_project AND project.archived_at IS NULL
 AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND version.review_state='accepted'
 AND source.id::text=p_source->'source'->>'artifact_id' AND source.sha256=p_source->'source'->>'sha256'
 AND version.content_hash=source.sha256 AND source.state='ready' AND source.verified_at IS NOT NULL
 AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND (t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',source.id,'sha256',source.sha256))
 OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.project_id=p_project
 AND i.task_id=t.id AND i.artifact_id=source.id AND i.sha256=source.sha256 AND i.contract='WorkingImage')));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_retouch_preview_landmark_origin(uuid,uuid,uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_landmark_origin(uuid,uuid,uuid,uuid,jsonb,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
