-- +goose Up
-- Exact source/candidate WorkingImage object for four-sided preview preparation.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_object(p_tenant uuid,p_task uuid,
 p_review uuid,p_result uuid,p_root uuid,p_root_sha text,p_side text,
 p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v28_restoration_preview_source(p_tenant,p_task) s
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=p_artifact
 WHERE p_side IN('before','after') AND s.review_id=p_review
  AND s.result_id=p_result AND s.root_id=p_root AND s.root_sha=p_root_sha
  AND ((p_side='before' AND s.source_id=p_artifact AND s.source_sha=p_sha)
    OR (p_side='after' AND s.candidate_id=p_artifact AND s.candidate_sha=p_sha))
  AND a.sha256=p_sha AND a.media_type='application/json'
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 1048576
  AND a.object_key IS NOT NULL AND a.object_key<>''
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_object(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_object(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_preview_object(uuid,uuid,uuid,uuid,uuid,text,text,uuid,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
