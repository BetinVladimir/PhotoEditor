-- +goose Up
-- Scheduler reads only exact adopted occurrences and the exact original manifest.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_preview_object(p_tenant uuid,p_task uuid,
 p_result uuid,p_attempt integer,p_fence bigint,p_root uuid,p_root_sha text,
 p_artifact uuid,p_sha text,p_path text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH source AS (
 SELECT s.* FROM photo.v29_perspective_preview_source(p_tenant,p_task) s
 WHERE s.result_id=p_result AND s.attempt=p_attempt AND s.fence=p_fence
  AND s.root_id=p_root AND s.root_sha=p_root_sha
 ), objects AS (
 SELECT o.* FROM source s
 CROSS JOIN LATERAL photo.v29_adopted_perspective_object(p_tenant,s.project_id,s.source_version_id,
  p_task,s.actor_id,s.auth_session_id,p_artifact,p_sha) o
 WHERE p_path='$' AND p_contract='ImageResult' AND p_artifact=s.root_id AND p_sha=s.root_sha
 UNION ALL
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type FROM source s
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=s.source_id AND a.sha256=s.source_sha
 WHERE p_path='$/source' AND p_contract='WorkingImage' AND p_artifact=s.source_id AND p_sha=s.source_sha
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
 UNION ALL
 SELECT o.* FROM source s
 CROSS JOIN LATERAL photo.v29_adopted_perspective_child_object(p_tenant,s.project_id,s.source_version_id,
  p_task,s.actor_id,s.auth_session_id,p_root,p_root_sha,p_artifact,p_sha,p_path,p_contract) o
 WHERE p_path NOT IN('$','$/source')
 ) SELECT * FROM objects;
$$;
ALTER FUNCTION photo.v29_perspective_preview_object(uuid,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_preview_object(uuid,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_preview_object(uuid,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
