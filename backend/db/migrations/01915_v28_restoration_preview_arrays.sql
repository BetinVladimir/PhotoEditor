-- +goose Up
-- Current physical NPY locator for the exact pending restoration review.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_array(p_tenant uuid,p_task uuid,
 p_review uuid,p_result uuid,p_root uuid,p_root_sha text,p_side text,
 p_channel text,p_image text,p_artifact uuid,p_sha text,p_bucket text)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s record; image jsonb; expected jsonb; a photo.artifacts%ROWTYPE;
 role text; locator jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_side IS NULL OR p_side NOT IN('before','after')
 OR p_channel IS NULL OR p_channel NOT IN('pixels','alpha')
 OR p_image IS NULL OR octet_length(p_image) NOT BETWEEN 2 AND 1048576
 OR p_artifact IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.v28_restoration_preview_source(p_tenant,p_task);
 IF NOT FOUND OR s.review_id IS DISTINCT FROM p_review OR s.result_id IS DISTINCT FROM p_result
 OR s.root_id IS DISTINCT FROM p_root OR s.root_sha IS DISTINCT FROM p_root_sha THEN RETURN NULL;END IF;
 IF encode(sha256(convert_to(p_image,'UTF8')),'hex') IS DISTINCT FROM
  (CASE p_side WHEN 'before' THEN s.source_sha ELSE s.candidate_sha END) THEN RETURN NULL;END IF;
 image:=p_image::jsonb;
 expected:=image->p_channel;
 IF jsonb_typeof(expected)<>'object' OR NOT expected ?& ARRAY['artifact_id','sha256']
 OR (expected-'artifact_id'-'sha256')<>'{}'::jsonb
 OR expected IS DISTINCT FROM jsonb_build_object('artifact_id',p_artifact,'sha256',p_sha)
 THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v28_restoration_preview_object(p_tenant,p_task,
  p_review,p_result,p_root,p_root_sha,p_side,
  CASE p_side WHEN 'before' THEN s.source_id ELSE s.candidate_id END,
  CASE p_side WHEN 'before' THEN s.source_sha ELSE s.candidate_sha END) o
  WHERE o.bucket=p_bucket AND o.bytes=octet_length(p_image)) THEN RETURN NULL;END IF;
 IF p_channel='pixels' THEN
  role:=CASE p_side WHEN 'before' THEN 'source_pixels' ELSE 'output_pixels' END;
  locator:=photo.v28_restoration_result_array(p_tenant,p_task,p_result,p_root,p_root_sha,
   role,p_artifact,p_sha,p_bucket);
  RETURN locator;
 END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact
  AND sha256=p_sha AND bucket=p_bucket AND state='ready'
  AND verified_at IS NOT NULL AND object_version NOT IN('','null')
  AND media_type IN('application/x-npy','application/octet-stream')
  AND bytes BETWEEN 14 AND 800065536
  AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND OR photo.v2_draft_artifact_scope(p_tenant,s.project_id,
  s.source_version_id,a.id,a.sha256,'WorkingAlpha',s.actor_id,s.auth_session_id,
  s.source_id,s.source_sha) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('key',a.object_key,'version',a.object_version,
  'sha256',a.sha256,'bytes',a.bytes,'media_type',a.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_array(uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_array(uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v28_restoration_preview_array(uuid,uuid,uuid,uuid,uuid,text,text,text,text,uuid,text,text)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
