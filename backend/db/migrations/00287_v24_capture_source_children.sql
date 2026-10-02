-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.capture_source_child(p_tenant uuid,p_job uuid,p_token uuid,p_child uuid,p_sha text,p_role text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT child.id,child.sha256,child.bytes,child.bucket,child.object_key,child.object_version,child.media_type
 FROM photo.capture_assessment_source(p_tenant,p_job,p_token) source
 JOIN photo.artifacts parent ON parent.tenant_id=p_tenant AND parent.id=source.artifact_id AND parent.sha256=source.sha256
 JOIN photo.artifacts child ON child.tenant_id=parent.tenant_id AND child.id=p_child AND child.sha256=p_sha
 WHERE parent.created_by_task_id IS NOT NULL AND parent.produced_attempt IS NOT NULL AND parent.produced_fence IS NOT NULL
 AND child.created_by_task_id=parent.created_by_task_id AND child.produced_attempt=parent.produced_attempt
 AND child.produced_fence=parent.produced_fence AND p_role IN('working_rgb','alpha') AND child.output_role=p_role
 AND child.media_type='application/octet-stream' AND child.state='ready' AND child.verified_at IS NOT NULL
 AND child.bytes>0 AND child.object_version IS NOT NULL AND child.object_version NOT IN('','null')
 AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_source_child(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_source_child(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_source_child(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
