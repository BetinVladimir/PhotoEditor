-- +goose Up
-- Exact immutable references from the canonical dispatched command only.
-- A task capability never grants arbitrary tenant-bucket access.
-- +goose StatementBegin
CREATE FUNCTION photo.gateway_input(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_artifact uuid)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
 FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate) permitted
 JOIN photo.tasks t ON t.tenant_id=permitted.tenant_id AND t.id=permitted.task_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=p_artifact
 WHERE a.state='ready' AND a.object_version IS NOT NULL AND a.object_version<>'' AND a.object_version<>'null'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS (
  SELECT 1 FROM jsonb_array_elements(
   coalesce(t.dispatch_command->'inputs','[]'::jsonb) || jsonb_build_array(t.dispatch_command->'params')
  ) ref WHERE ref->>'artifact_id'=a.id::text AND ref->>'sha256'=a.sha256
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) TO photo_gateway;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
