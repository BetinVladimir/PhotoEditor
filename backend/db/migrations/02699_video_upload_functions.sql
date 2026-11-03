-- +goose Up
-- FN-D-01..05: video upload lifecycle. Quota follows the style-package upload pattern:
-- one quota_reservations row (holder_type video_upload) plus tenant_usage.reserved_bytes.
-- Defaults of DS-01: VIDEO_MAX_ACTIVE_UPLOADS=3, UPLOAD_BEGINS_PER_HOUR=120, UPLOAD_IDLE_EXPIRY_DAYS=7,
-- DERIV_EST_CAP_BYTES+DERIV_EST_BASE_BYTES bound the derivative part of the reservation.

-- +goose StatementBegin
CREATE FUNCTION photo.begin_video_upload(p_tenant uuid,p_project uuid,p_principal uuid,p_media_kind text,p_filename text,
 p_bytes bigint,p_sha text,p_request_sha text,p_part_bytes bigint,p_parts_total integer,p_object_key text,p_reserve_bytes bigint)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.video_upload_sessions%ROWTYPE;lim photo.video_tenant_limits%ROWTYPE;max_active integer;
 used bigint;reserved bigint;quota bigint;rid uuid;sid uuid;begins integer;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_principal IS NULL OR p_bytes IS NULL OR p_reserve_bytes IS NULL
  OR p_reserve_bytes<p_bytes OR p_reserve_bytes>p_bytes+4294967296+134217728 THEN
  RAISE EXCEPTION 'VIDEO_UPLOAD_INVALID' USING ERRCODE='22023';END IF;
 SELECT * INTO s FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND project_id=p_project AND principal_id=p_principal AND request_sha256=p_request_sha;
 IF FOUND THEN
  IF s.declared_sha256<>p_sha OR s.declared_bytes<>p_bytes OR s.filename<>p_filename OR s.media_kind<>p_media_kind THEN
   RAISE EXCEPTION 'IDEMPOTENCY_KEY_REUSED' USING ERRCODE='23505';END IF;
  RETURN s.id;END IF;
 IF NOT EXISTS(SELECT FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL) THEN
  RAISE EXCEPTION 'PROJECT_NOT_FOUND' USING ERRCODE='P0002';END IF;
 SELECT u.storage_bytes,u.reserved_bytes,(t.quotas->>'storage_bytes')::bigint INTO used,reserved,quota
  FROM photo.tenant_usage u JOIN photo.tenants t ON t.id=u.tenant_id WHERE u.tenant_id=p_tenant AND t.status='active' FOR UPDATE OF u;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_NOT_ACTIVE' USING ERRCODE='42501';END IF;
 SELECT * INTO lim FROM photo.video_tenant_limits WHERE tenant_id=p_tenant;
 max_active:=coalesce(lim.max_active_uploads,3);
 IF (SELECT count(*) FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND state IN('initializing','uploading','completing'))>=max_active THEN
  RAISE EXCEPTION 'VIDEO_UPLOAD_LIMIT' USING ERRCODE='53400';END IF;
 INSERT INTO photo.video_tenant_rate(tenant_id,hour_start,upload_begins) VALUES(p_tenant,date_trunc('hour',clock_timestamp()),1)
  ON CONFLICT(tenant_id,hour_start) DO UPDATE SET upload_begins=photo.video_tenant_rate.upload_begins+1 RETURNING upload_begins INTO begins;
 IF begins>120 THEN RAISE EXCEPTION 'VIDEO_UPLOAD_RATE' USING ERRCODE='53400';END IF;
 IF quota IS NULL OR quota<p_reserve_bytes OR used>quota-p_reserve_bytes OR reserved>quota-p_reserve_bytes-used THEN
  RAISE EXCEPTION 'QUOTA_EXCEEDED' USING ERRCODE='53400';END IF;
 sid:=gen_random_uuid();rid:=gen_random_uuid();
 INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at)
  VALUES(rid,p_tenant,'user',p_principal,'video_upload',sid,p_reserve_bytes,0,'reserved',clock_timestamp()+interval '7 days');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_reserve_bytes WHERE tenant_id=p_tenant;
 INSERT INTO photo.video_upload_sessions(id,tenant_id,project_id,principal_id,media_kind,filename,declared_bytes,declared_sha256,request_sha256,
  part_bytes,parts_total,object_key,state,reservation_id,expires_at)
  VALUES(sid,p_tenant,p_project,p_principal,p_media_kind,p_filename,p_bytes,p_sha,p_request_sha,p_part_bytes,p_parts_total,p_object_key,
  'initializing',rid,clock_timestamp()+interval '7 days');
 RETURN sid;
END $$;
-- +goose StatementEnd

-- The API stores the multipart id after the object store accepted the multipart creation.
-- +goose StatementBegin
CREATE FUNCTION photo.bind_video_upload_multipart(p_tenant uuid,p_upload uuid,p_multipart text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 THEN RETURN false;END IF;
 UPDATE photo.video_upload_sessions SET multipart_id=p_multipart,state='uploading',revision=revision+1,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=p_upload AND state='initializing' AND multipart_id IS NULL;
 RETURN FOUND OR EXISTS(SELECT FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND id=p_upload AND multipart_id=p_multipart);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.record_video_upload_part(p_tenant uuid,p_upload uuid,p_part integer,p_etag text,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.video_upload_sessions%ROWTYPE;expected bigint;old_etag text;
BEGIN
 SELECT * INTO s FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR s.state<>'uploading' OR s.expires_at<=clock_timestamp() OR p_part NOT BETWEEN 1 AND s.parts_total THEN RETURN false;END IF;
 expected:=CASE WHEN p_part<s.parts_total THEN s.part_bytes ELSE s.declared_bytes-(s.parts_total-1)*s.part_bytes END;
 IF p_bytes IS DISTINCT FROM expected OR p_etag IS NULL THEN RETURN false;END IF;
 SELECT etag INTO old_etag FROM photo.video_upload_parts WHERE tenant_id=p_tenant AND upload_id=p_upload AND part_no=p_part;
 IF FOUND THEN RETURN old_etag=p_etag;END IF;
 INSERT INTO photo.video_upload_parts(tenant_id,upload_id,part_no,etag,bytes) VALUES(p_tenant,p_upload,p_part,p_etag,p_bytes);
 UPDATE photo.video_upload_sessions SET expires_at=clock_timestamp()+interval '7 days',updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
-- +goose StatementEnd

-- API complete: the client's part list must equal the recorded one; returns the completion token for the scheduler.
-- +goose StatementBegin
CREATE FUNCTION photo.complete_video_upload(p_tenant uuid,p_upload uuid,p_parts jsonb) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.video_upload_sessions%ROWTYPE;tok uuid;
BEGIN
 SELECT * INTO s FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_NOT_FOUND' USING ERRCODE='P0002';END IF;
 IF s.state IN('completing','verifying','verified') THEN RETURN s.completion_token;END IF;
 IF s.state='expired' OR s.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'UPLOAD_EXPIRED' USING ERRCODE='22023';END IF;
 IF s.state<>'uploading' OR p_parts IS NULL OR jsonb_typeof(p_parts)<>'array' THEN RAISE EXCEPTION 'UPLOAD_STATE' USING ERRCODE='22023';END IF;
 IF jsonb_array_length(p_parts)<>s.parts_total
  OR (SELECT count(*) FROM photo.video_upload_parts WHERE tenant_id=p_tenant AND upload_id=p_upload)<>s.parts_total
  OR EXISTS(SELECT FROM jsonb_to_recordset(p_parts) AS x(part_no integer,etag text)
   LEFT JOIN photo.video_upload_parts r ON r.tenant_id=p_tenant AND r.upload_id=p_upload AND r.part_no=x.part_no AND r.etag=x.etag WHERE r.part_no IS NULL)
  OR (SELECT sum(bytes) FROM photo.video_upload_parts WHERE tenant_id=p_tenant AND upload_id=p_upload)<>s.declared_bytes THEN
  RAISE EXCEPTION 'UPLOAD_PARTS_MISMATCH' USING ERRCODE='22023';END IF;
 tok:=gen_random_uuid();
 UPDATE photo.video_upload_sessions SET state='completing',completion_token=tok,completion_lease_until=clock_timestamp()+interval '10 minutes',
  revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN tok;
END $$;
-- +goose StatementEnd

-- Scheduler: the object store assembled the multipart object and computed its SHA-256.
-- Hash mismatch rejects the upload and releases the reservation.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_video_upload_completion(p_tenant uuid,p_upload uuid,p_token uuid,p_bucket text,p_version text,p_sha text,p_media_type text)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.video_upload_sessions%ROWTYPE;aid uuid;mid uuid;r photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024
  OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_media_type IS NULL OR length(p_media_type) NOT BETWEEN 3 AND 255 THEN RETURN NULL;END IF;
 SELECT * INTO s FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF s.state='verifying' THEN RETURN (SELECT id FROM photo.video_media WHERE tenant_id=p_tenant AND upload_id=p_upload);END IF;
 IF s.state<>'completing' OR s.completion_token IS DISTINCT FROM p_token OR s.completion_lease_until<=clock_timestamp() THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.quota_reservations WHERE tenant_id=p_tenant AND id=s.reservation_id AND state='reserved' AND holder_type='video_upload' AND holder_id=s.id FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 IF p_sha<>s.declared_sha256 THEN
  UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=p_tenant AND id=r.id;
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes WHERE tenant_id=p_tenant;
  UPDATE photo.video_upload_sessions SET state='rejected',reject_code='UPLOAD_HASH_MISMATCH',completion_token=NULL,completion_lease_until=NULL,
   revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_upload;
  RETURN NULL;END IF;
 aid:=gen_random_uuid();mid:=gen_random_uuid();
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
  VALUES(aid,p_tenant,p_bucket,s.object_key,p_version,p_sha,s.declared_bytes,p_media_type,'ready',clock_timestamp(),
  CASE s.media_kind WHEN 'audio' THEN 'audio_original' ELSE 'video_original' END);
 INSERT INTO photo.video_media(id,tenant_id,project_id,created_by,kind,name,state,upload_id,source_artifact_id,source_sha256,source_bytes)
  VALUES(mid,p_tenant,s.project_id,s.principal_id,s.media_kind,s.filename,'probing',s.id,aid,p_sha,s.declared_bytes);
 UPDATE photo.video_upload_sessions SET state='verifying',object_version=p_version,completion_token=NULL,completion_lease_until=NULL,
  revision=revision+1,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN mid;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.abort_video_upload(p_tenant uuid,p_upload uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s photo.video_upload_sessions%ROWTYPE;r photo.quota_reservations%ROWTYPE;
BEGIN
 SELECT * INTO s FROM photo.video_upload_sessions WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF s.state='aborted' THEN RETURN true;END IF;
 IF s.state NOT IN('initializing','uploading','completing') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.quota_reservations WHERE tenant_id=p_tenant AND id=s.reservation_id AND state='reserved' FOR UPDATE;
 IF FOUND THEN
  UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=p_tenant AND id=r.id;
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes WHERE tenant_id=p_tenant;END IF;
 DELETE FROM photo.video_upload_parts WHERE tenant_id=p_tenant AND upload_id=p_upload;
 UPDATE photo.video_upload_sessions SET state='aborted',completion_token=NULL,completion_lease_until=NULL,revision=revision+1,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.expire_video_uploads(p_limit integer) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s record;n integer:=0;r photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 1000 THEN RAISE EXCEPTION 'LIMIT_INVALID' USING ERRCODE='22023';END IF;
 FOR s IN SELECT tenant_id,id,reservation_id FROM photo.video_upload_sessions
  WHERE state IN('initializing','uploading','completing') AND expires_at<=clock_timestamp() ORDER BY expires_at LIMIT p_limit FOR UPDATE SKIP LOCKED LOOP
  SELECT * INTO r FROM photo.quota_reservations WHERE tenant_id=s.tenant_id AND id=s.reservation_id AND state='reserved' FOR UPDATE;
  IF FOUND THEN
   UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=s.tenant_id AND id=r.id;
   UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-r.bytes WHERE tenant_id=s.tenant_id;END IF;
  UPDATE photo.video_upload_sessions SET state='expired',completion_token=NULL,completion_lease_until=NULL,revision=revision+1,updated_at=clock_timestamp()
   WHERE tenant_id=s.tenant_id AND id=s.id;
  n:=n+1;
 END LOOP;
 RETURN n;
END $$;
-- +goose StatementEnd

-- Ownership and grants (R16).
ALTER FUNCTION photo.begin_video_upload(uuid,uuid,uuid,text,text,bigint,text,text,bigint,integer,text,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.bind_video_upload_multipart(uuid,uuid,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.record_video_upload_part(uuid,uuid,integer,text,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.complete_video_upload(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.finish_video_upload_completion(uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.abort_video_upload(uuid,uuid) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.expire_video_uploads(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_video_upload(uuid,uuid,uuid,text,text,bigint,text,text,bigint,integer,text,bigint),
 photo.bind_video_upload_multipart(uuid,uuid,text),photo.record_video_upload_part(uuid,uuid,integer,text,bigint),
 photo.complete_video_upload(uuid,uuid,jsonb),photo.finish_video_upload_completion(uuid,uuid,uuid,text,text,text,text),
 photo.abort_video_upload(uuid,uuid),photo.expire_video_uploads(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_video_upload(uuid,uuid,uuid,text,text,bigint,text,text,bigint,integer,text,bigint),
 photo.bind_video_upload_multipart(uuid,uuid,text),photo.record_video_upload_part(uuid,uuid,integer,text,bigint),
 photo.complete_video_upload(uuid,uuid,jsonb),photo.abort_video_upload(uuid,uuid) TO photo_api;
GRANT EXECUTE ON FUNCTION photo.finish_video_upload_completion(uuid,uuid,uuid,text,text,text,text) TO photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.expire_video_uploads(integer) TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
