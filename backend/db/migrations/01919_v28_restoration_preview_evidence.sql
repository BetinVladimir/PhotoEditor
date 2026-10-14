-- +goose Up
-- A scheduler-only context and four-object evidence surface. Every read
-- inherits the current pending-review/source gate from the result function.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_context(p_tenant uuid,p_run uuid,p_side text,p_variant text)
RETURNS TABLE(task_id uuid,result_id uuid,attempt integer,fence bigint,
 params_id uuid,params_sha text,manifest_id uuid,manifest_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT o.task_id,o.result_id,o.attempt,o.fence,d.artifact_id,d.sha256,
  o.artifact_id,o.sha256
 FROM photo.v28_restoration_preview_result(p_tenant,p_run,p_side,p_variant) o
 JOIN photo.backend_documents d ON d.tenant_id=p_tenant AND d.run_id=p_run
  AND d.owner_id=o.task_id AND d.artifact_id=(SELECT t.params_artifact_id
   FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=o.task_id)
  AND d.purpose='task_params' AND d.state='ready'
 WHERE o.output_role='derivative_manifest';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_context(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_context(uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_preview_context(uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_evidence(p_tenant uuid,p_run uuid,
 p_side text,p_variant text,p_result uuid,p_attempt integer,p_fence bigint,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,
 bytes bigint,media_type text,role text,source text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH outputs AS (
  SELECT * FROM photo.v28_restoration_preview_result(p_tenant,p_run,p_side,p_variant)
  WHERE result_id=p_result AND attempt=p_attempt AND fence=p_fence
 ), current_task AS (
  SELECT DISTINCT task_id FROM outputs
 )
 SELECT o.bucket,o.object_key,o.object_version,o.sha256,o.bytes,o.media_type,
  o.output_role,'output'::text FROM outputs o WHERE o.artifact_id=p_artifact
 UNION ALL
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
  CASE WHEN a.id=t.params_artifact_id THEN 'task_params' ELSE a.output_role END,
  'input'::text
 FROM current_task c JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=c.task_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 WHERE (a.id=t.params_artifact_id OR t.inputs @> jsonb_build_array(
  jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_evidence(uuid,uuid,text,text,uuid,integer,bigint,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_evidence(uuid,uuid,text,text,uuid,integer,bigint,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_preview_evidence(uuid,uuid,text,text,uuid,integer,bigint,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
