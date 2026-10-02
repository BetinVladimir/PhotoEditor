-- +goose Up
-- Read only output artifacts pinned by adoption of the canonical preparation.
-- The caller obtains refs from the hash-verified root/WorkingImage document.
-- +goose StatementBegin
CREATE FUNCTION photo.capture_prepared_artifact(p_tenant uuid,p_run uuid,p_artifact uuid,p_sha text,p_role text)
RETURNS TABLE(artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.capture_prepared_source(p_tenant,p_run) root
 JOIN photo.tasks task ON task.tenant_id=p_tenant AND task.id=root.task_id
 JOIN photo.v2_workflows w ON w.tenant_id=task.tenant_id AND w.run_id=task.run_id
 JOIN photo.artifacts a ON a.tenant_id=task.tenant_id AND a.id=p_artifact AND a.sha256=p_sha
  AND a.created_by_task_id=task.id AND a.produced_attempt=task.attempt AND a.produced_fence=task.fencing_token
 JOIN photo.output_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id AND u.task_id=task.id
  AND u.attempt=task.attempt AND u.fence=task.fencing_token AND u.state='ready' AND u.sha256=a.sha256 AND u.role=p_role
 WHERE p_role IN('working_image','faces','working_rgb','working_alpha')
  AND a.output_role=p_role AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.v2_artifact_links link WHERE link.tenant_id=a.tenant_id
   AND link.workflow_id=w.id AND link.artifact_id=a.id AND link.sha256=a.sha256);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_prepared_artifact(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_prepared_artifact(uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_prepared_artifact(uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
