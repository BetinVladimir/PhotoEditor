-- +goose Up
-- Intrinsic dimensions are measured by the gateway from the immutable byte stream,
-- never supplied by HTTP headers/worker JSON. Existing output authority and
-- quota/fence functions remain unchanged and execute in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_output_image(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,
 p_key uuid,p_role text,p_bytes bigint,p_sha text,p_media text,p_bucket text)
RETURNS TABLE(outcome text,upload_id uuid,artifact_id uuid,object_key text,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE receipt record;a photo.artifacts%ROWTYPE;u photo.output_uploads%ROWTYPE;
BEGIN
 IF p_media IS NULL OR p_media NOT IN('image/jpeg','image/png','image/webp') THEN
  RAISE EXCEPTION 'INTRINSIC_IMAGE_MEDIA_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO receipt FROM photo.begin_output(p_task,p_attempt,p_fence,p_worker,p_certificate,p_key,p_role,p_bytes,p_sha,p_media,p_bucket);
 IF receipt.outcome='replayed' THEN
  SELECT upload.* INTO u FROM photo.output_uploads upload WHERE upload.id=receipt.upload_id;
  SELECT artifact.* INTO a FROM photo.artifacts artifact WHERE artifact.id=receipt.artifact_id AND artifact.created_by_task_id=p_task
   AND artifact.produced_attempt=p_attempt AND artifact.produced_fence=p_fence AND artifact.output_role=p_role
   AND artifact.state='ready' AND artifact.bucket=p_bucket AND artifact.object_key=receipt.object_key
   AND artifact.object_version=u.object_version AND artifact.object_version NOT IN('','null')
   AND artifact.sha256=p_sha AND artifact.bytes=p_bytes AND artifact.media_type=p_media
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp());
  IF a.id IS NULL OR a.tenant_id IS DISTINCT FROM u.tenant_id
   OR a.width IS NULL OR a.height IS NULL OR a.width NOT BETWEEN 1 AND 200000
   OR a.height NOT BETWEEN 1 AND 200000 OR a.width::bigint*a.height::bigint>200000000 THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;
  END IF;
 END IF;
 RETURN QUERY SELECT receipt.outcome,receipt.upload_id,receipt.artifact_id,receipt.object_key,receipt.sha256;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_auth,photo_publisher,photo_maintenance,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.begin_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) TO photo_gateway;

-- +goose StatementBegin
DO $install$
DECLARE old text;candidate text;inverse text;
BEGIN
 SELECT prosrc INTO old FROM pg_proc WHERE oid='photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text)'::regprocedure;
 IF md5(old) IS DISTINCT FROM 'f50bd8cba853d5ff222934851c8a0093' THEN RAISE EXCEPTION 'IMAGE_COMPLETE_PREIMAGE_CHANGED';END IF;
 candidate:=old;
 IF length(candidate)-length(replace(candidate,'BEGIN
',''))<>length('BEGIN
') THEN RAISE EXCEPTION 'IMAGE_COMPLETE_ANCHOR_CHANGED';END IF;
 candidate:=replace(candidate,'BEGIN
','BEGIN
 IF p_media IS NULL OR p_media NOT IN(''image/jpeg'',''image/png'',''image/webp'')
 OR p_width IS NULL OR p_height IS NULL OR p_width NOT BETWEEN 1 AND 200000
 OR p_height NOT BETWEEN 1 AND 200000 OR p_width::bigint*p_height::bigint>200000000 THEN
 RAISE EXCEPTION ''INTRINSIC_IMAGE_METADATA_INVALID'' USING ERRCODE=''22023''; END IF;
');
 IF length(candidate)-length(replace(candidate,'verified_at,output_role)',''))<>length('verified_at,output_role)') THEN RAISE EXCEPTION 'IMAGE_COMPLETE_ANCHOR_CHANGED';END IF;
 candidate:=replace(candidate,'verified_at,output_role)','verified_at,output_role,width,height)');
 IF length(candidate)-length(replace(candidate,'clock_timestamp(),u.role);',''))<>length('clock_timestamp(),u.role);') THEN RAISE EXCEPTION 'IMAGE_COMPLETE_ANCHOR_CHANGED';END IF;
 candidate:=replace(candidate,'clock_timestamp(),u.role);','clock_timestamp(),u.role,p_width,p_height);');
 IF length(candidate)-length(replace(candidate,'IF u.object_version=p_version THEN RETURN QUERY SELECT ''replayed'',u.artifact_id,u.sha256;',''))<>length('IF u.object_version=p_version THEN RETURN QUERY SELECT ''replayed'',u.artifact_id,u.sha256;') THEN RAISE EXCEPTION 'IMAGE_COMPLETE_ANCHOR_CHANGED';END IF;
 candidate:=replace(candidate,'IF u.object_version=p_version THEN RETURN QUERY SELECT ''replayed'',u.artifact_id,u.sha256;','IF u.object_version=p_version THEN
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts artifact WHERE artifact.id=u.artifact_id
   AND artifact.tenant_id=u.tenant_id AND artifact.created_by_task_id=p_task
   AND artifact.produced_attempt=p_attempt AND artifact.produced_fence=p_fence
   AND artifact.output_role=u.role AND artifact.bucket=u.bucket AND artifact.object_key=u.object_key
   AND artifact.object_version=p_version AND artifact.sha256=p_sha AND artifact.bytes=p_bytes
   AND artifact.media_type=p_media AND artifact.state=''ready''
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
   AND artifact.width=p_width AND artifact.height=p_height) THEN
   RAISE EXCEPTION ''INTRINSIC_IMAGE_METADATA_CONFLICT'' USING ERRCODE=''23514'';
  END IF;
  RETURN QUERY SELECT ''replayed'',u.artifact_id,u.sha256;');
 inverse:=candidate;
 inverse:=replace(inverse,'IF u.object_version=p_version THEN
  IF NOT EXISTS(SELECT 1 FROM photo.artifacts artifact WHERE artifact.id=u.artifact_id
   AND artifact.tenant_id=u.tenant_id AND artifact.created_by_task_id=p_task
   AND artifact.produced_attempt=p_attempt AND artifact.produced_fence=p_fence
   AND artifact.output_role=u.role AND artifact.bucket=u.bucket AND artifact.object_key=u.object_key
   AND artifact.object_version=p_version AND artifact.sha256=p_sha AND artifact.bytes=p_bytes
   AND artifact.media_type=p_media AND artifact.state=''ready''
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp())
   AND artifact.width=p_width AND artifact.height=p_height) THEN
   RAISE EXCEPTION ''INTRINSIC_IMAGE_METADATA_CONFLICT'' USING ERRCODE=''23514'';
  END IF;
  RETURN QUERY SELECT ''replayed'',u.artifact_id,u.sha256;','IF u.object_version=p_version THEN RETURN QUERY SELECT ''replayed'',u.artifact_id,u.sha256;');
 inverse:=replace(inverse,'clock_timestamp(),u.role,p_width,p_height);','clock_timestamp(),u.role);');
 inverse:=replace(inverse,'verified_at,output_role,width,height)','verified_at,output_role)');
 inverse:=replace(inverse,'BEGIN
 IF p_media IS NULL OR p_media NOT IN(''image/jpeg'',''image/png'',''image/webp'')
 OR p_width IS NULL OR p_height IS NULL OR p_width NOT BETWEEN 1 AND 200000
 OR p_height NOT BETWEEN 1 AND 200000 OR p_width::bigint*p_height::bigint>200000000 THEN
 RAISE EXCEPTION ''INTRINSIC_IMAGE_METADATA_INVALID'' USING ERRCODE=''22023''; END IF;
','BEGIN
');
 IF inverse IS DISTINCT FROM old THEN RAISE EXCEPTION 'IMAGE_COMPLETE_INVERSE_CHANGED';END IF;
 EXECUTE format('CREATE FUNCTION photo.complete_output_image(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_upload uuid,p_version text,p_bytes bigint,p_sha text,p_media text,p_width integer,p_height integer) RETURNS TABLE(outcome text,artifact_id uuid,sha256 text) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS %L',candidate);
END $install$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,integer,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,integer,integer) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_auth,photo_publisher,photo_maintenance,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.complete_output_image(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,integer,integer) TO photo_gateway;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
