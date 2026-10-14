-- +goose Up
-- Dedicated evidence for collectionless Render derivatives, no broad table reads.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_preview_evidence(p_tenant uuid,p_run uuid,p_variant text,p_result uuid,p_attempt integer,p_fence bigint,p_artifact uuid)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text,role text,source text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH outputs AS (
 SELECT * FROM photo.v29_perspective_preview_result(p_tenant,p_run,p_variant)
 WHERE result_id=p_result AND attempt=p_attempt AND fence=p_fence
 ), current_task AS (
 SELECT DISTINCT task_id FROM outputs
 )
 SELECT o.bucket,o.object_key,o.object_version,o.sha256,o.bytes,o.media_type,o.output_role,'output'::text
 FROM outputs o WHERE o.artifact_id=p_artifact
 UNION ALL
 SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type,
 CASE WHEN a.id=t.params_artifact_id THEN 'task_params' ELSE a.output_role END,'input'::text
 FROM current_task c JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.id=c.task_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 WHERE (a.id=t.params_artifact_id OR t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)))
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
 AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_perspective_preview_evidence(uuid,uuid,text,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_preview_evidence(uuid,uuid,text,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_preview_evidence(uuid,uuid,text,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
