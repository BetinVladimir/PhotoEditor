-- +goose Up
-- Completed-result source scope for scheduler adoption; no live worker lease required.
-- Object bytes, canvas, ICC, scene and protected-mask proof remain separate gates.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_adoption_source_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_version uuid,p_source uuid,p_source_sha text,p_target boolean)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; session_id uuid; owner_run uuid;
BEGIN
 IF p_target IS NULL OR p_source_sha IS NULL OR p_source_sha !~ '^[0-9a-f]{64}$'
 OR NOT photo.v27_semantic_adoption_qualification_ready(p_tenant,p_task,p_attempt,p_fence,p_result) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v27_semantic_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result)) THEN RETURN false;END IF;
 SELECT run_id INTO owner_run FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=owner_run;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=owner_run;
 session_id:=(r.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid;
 IF w.id IS NULL OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR NOT photo.v25_lens_snapshot_ready(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,session_id) THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.asset_versions v
 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
 JOIN photo.artifacts original ON original.tenant_id=a.tenant_id AND original.id=a.original_artifact_id
 JOIN photo.v2_task_inputs declared ON declared.tenant_id=source.tenant_id AND declared.task_id=p_task AND declared.artifact_id=source.id AND declared.sha256=source.sha256 AND declared.contract='WorkingImage'
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha AND v.review_state='accepted'
 AND a.project_id=w.project_id AND a.deleted_at IS NULL AND a.upload_state='verified'
 AND source.state='ready' AND source.verified_at IS NOT NULL AND source.output_role='working_image'
 AND source.media_type='application/json' AND source.bytes BETWEEN 2 AND 1048576
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null') AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND original.state='ready' AND original.verified_at IS NOT NULL AND original.bytes>0
 AND original.object_version IS NOT NULL AND original.object_version NOT IN('','null') AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
 AND (NOT p_target OR EXISTS(SELECT 1 FROM photo.snapshot_items i WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id AND i.asset_version_id=v.id AND i.artifact_id=source.id AND i.sha256=source.sha256)));
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
ALTER FUNCTION photo.v27_semantic_adoption_source_ready(uuid,uuid,integer,bigint,uuid,uuid,uuid,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_adoption_source_ready(uuid,uuid,integer,bigint,uuid,uuid,uuid,text,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_adoption_source_ready(uuid,uuid,integer,bigint,uuid,uuid,uuid,text,boolean) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
