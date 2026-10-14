-- +goose Up
-- Exact original-array membership comes from original SHA-bound WorkingImage bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_preview_original_array(p_tenant uuid,p_task uuid,p_result uuid,
 p_attempt integer,p_fence bigint,p_source text,p_artifact uuid,p_sha text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source record; manifest jsonb; expected jsonb;
BEGIN
 IF p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 65536
  OR p_contract IS NULL OR p_contract NOT IN('WorkingPixels','WorkingAlpha') THEN RETURN;END IF;
 SELECT * INTO source FROM photo.v29_perspective_preview_source(p_tenant,p_task);
 IF NOT FOUND OR source.result_id IS DISTINCT FROM p_result OR source.attempt IS DISTINCT FROM p_attempt
  OR source.fence IS DISTINCT FROM p_fence
  OR encode(sha256(convert_to(p_source,'UTF8')),'hex') IS DISTINCT FROM source.source_sha THEN RETURN;END IF;
 manifest:=p_source::jsonb;
 expected:=CASE p_contract WHEN 'WorkingPixels' THEN manifest->'pixels' ELSE manifest->'alpha' END;
 IF expected IS DISTINCT FROM jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha) THEN RETURN;END IF;
 RETURN QUERY SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version NOT IN('','null')
  AND a.bytes BETWEEN 1 AND 2400065536 AND a.media_type IN('application/x-npy','application/octet-stream')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND photo.v2_draft_artifact_scope(p_tenant,source.project_id,source.source_version_id,
   a.id,a.sha256,p_contract,source.actor_id,source.auth_session_id,source.source_id,source.source_sha);
EXCEPTION WHEN invalid_text_representation THEN RETURN;
END $$;
ALTER FUNCTION photo.v29_perspective_preview_original_array(uuid,uuid,uuid,integer,bigint,text,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_preview_original_array(uuid,uuid,uuid,integer,bigint,text,uuid,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_preview_original_array(uuid,uuid,uuid,integer,bigint,text,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
