-- +goose Up
-- Guest upload creation resolves every scope value from the authenticated share
-- session. The caller supplies only validated file metadata and generated IDs.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_portal_upload(p_session_hash bytea,p_csrf_hash bytea,p_key text,p_request_hash text,
 p_upload uuid,p_reservation uuid,p_object_key text,p_filename text,p_bytes bigint,p_format text,p_media_type text,p_assume_profile text,p_expected_sha text,p_expires timestamptz)
RETURNS TABLE(outcome text,id uuid,project_id uuid,collection_id uuid,state text,revision bigint,filename text,expected_size bigint,
 actual_size bigint,expires_at timestamptz,object_key text,multipart_id text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE sh photo.share_sessions%ROWTYPE;l photo.share_links%ROWTYPE;c photo.collections%ROWTYPE;u photo.upload_sessions%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE;storage bigint;reserved bigint;files bigint;reserved_files bigint;byte_limit numeric;file_limit numeric;
BEGIN
 IF octet_length(p_session_hash)<>32 OR octet_length(p_csrf_hash)<>32 OR p_key !~ '^[!-~]{1,128}$'
 OR p_request_hash !~ '^[0-9a-f]{64}$' OR p_upload IS NULL OR p_reservation IS NULL OR p_upload=p_reservation
 OR p_object_key IS NULL OR length(p_object_key) NOT BETWEEN 1 AND 1024 OR p_filename IS NULL OR length(p_filename) NOT BETWEEN 1 AND 255
 OR p_bytes NOT BETWEEN 1 AND 536870912 OR p_format !~ '^[a-z0-9]{2,8}$' OR p_media_type !~ '^[a-z0-9.+-]+/[a-z0-9.+-]+$'
 OR (p_assume_profile IS NOT NULL AND p_assume_profile NOT IN ('srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'))
 OR (p_expected_sha IS NOT NULL AND p_expected_sha !~ '^[0-9a-f]{64}$') OR p_expires IS NULL OR p_expires<=clock_timestamp()
 OR p_expires>clock_timestamp()+interval '24 hours' THEN
  RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;
 END IF;
 SELECT * INTO sh FROM photo.share_sessions WHERE session_hash=p_session_hash;
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;END IF;
 SELECT tu.storage_bytes,tu.reserved_bytes,tu.source_files,tu.reserved_files,(t.quotas->>'storage_bytes')::numeric,(t.quotas->>'source_files')::numeric
 INTO storage,reserved,files,reserved_files,byte_limit,file_limit FROM photo.tenant_usage tu JOIN photo.tenants t ON t.id=tu.tenant_id WHERE tu.tenant_id=sh.tenant_id FOR UPDATE OF tu;
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO l FROM photo.share_links WHERE tenant_id=sh.tenant_id AND id=sh.share_id FOR UPDATE;
 SELECT * INTO sh FROM photo.share_sessions WHERE tenant_id=sh.tenant_id AND id=sh.id FOR UPDATE;
 SELECT * INTO c FROM photo.collections WHERE tenant_id=sh.tenant_id AND id=sh.collection_id AND deleted_at IS NULL FOR UPDATE;
 IF l.id IS NULL OR c.id IS NULL OR sh.csrf_hash<>p_csrf_hash OR sh.revoked_at IS NOT NULL OR sh.expires_at<=clock_timestamp()
 OR l.collection_id<>sh.collection_id OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp())
 OR l.policy_version<>sh.link_version OR l.permissions->'upload'<>'true'::jsonb OR c.state='archived'
 OR jsonb_typeof(l.limits)<>'object' OR jsonb_typeof(l.limits->'max_upload_files')<>'number'
 OR jsonb_typeof(l.limits->'max_upload_bytes_total')<>'number' OR jsonb_typeof(l.limits->'max_file_bytes')<>'number'
 OR jsonb_typeof(l.limits->'allowed_formats')<>'array' OR NOT (l.limits->'allowed_formats' ? p_format) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at)
 VALUES(sh.tenant_id,'guest',sh.id,'Upload.BeginGuest',p_key,p_request_hash,least(sh.expires_at,clock_timestamp()+interval '30 days'))
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=sh.tenant_id AND x.principal_type='guest' AND x.principal_id=sh.id AND x.route='Upload.BeginGuest' AND x.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_hash THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;END IF;
 IF idem.response_status IS NOT NULL THEN
  SELECT * INTO u FROM photo.upload_sessions x WHERE x.tenant_id=sh.tenant_id AND x.id=idem.resource_id AND x.principal_type='guest' AND x.principal_id=sh.id AND x.share_id=l.id;
  IF NOT FOUND THEN RAISE EXCEPTION 'PORTAL_UPLOAD_REPLAY_MISSING' USING ERRCODE='23514';END IF;
  RETURN QUERY SELECT 'replayed',u.id,u.project_id,u.collection_id,u.state,u.revision,u.filename,u.expected_size,u.actual_size,u.expires_at,u.object_key,u.multipart_id,u.media_type;RETURN;
 END IF;
 IF p_bytes>(l.limits->>'max_file_bytes')::numeric OR l.used_bytes::numeric+l.reserved_bytes+p_bytes>(l.limits->>'max_upload_bytes_total')::numeric
 OR l.used_files::numeric+l.reserved_files+1>(l.limits->>'max_upload_files')::numeric
 OR storage::numeric+reserved+p_bytes>byte_limit OR files::numeric+reserved_files+1>file_limit
 OR storage::numeric+reserved+p_bytes>9007199254740991 OR files::numeric+reserved_files+1>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::uuid,NULL::text,NULL::bigint,NULL::text,NULL::bigint,NULL::bigint,NULL::timestamptz,NULL::text,NULL::text,NULL::text;RETURN;
 END IF;
 INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,share_id,policy_version,expected_size,filename,assume_profile,object_key,state,expires_at,reservation_id,media_type,expected_sha256)
 VALUES(p_upload,sh.tenant_id,c.project_id,c.id,'guest',sh.id,l.id,l.policy_version,p_bytes,p_filename,p_assume_profile,p_object_key,'initializing',least(p_expires,sh.expires_at),p_reservation,p_media_type,p_expected_sha);
 INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,share_id,holder_type,holder_id,bytes,files,state,expires_at)
 VALUES(p_reservation,sh.tenant_id,'guest',sh.id,l.id,'upload',p_upload,p_bytes,1,'reserved',least(p_expires,sh.expires_at));
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes,reserved_files=reserved_files+1 WHERE tenant_id=sh.tenant_id;
 UPDATE photo.share_links SET reserved_bytes=reserved_bytes+p_bytes,reserved_files=reserved_files+1 WHERE tenant_id=sh.tenant_id AND id=l.id;
 UPDATE photo.idempotency_records SET response_status=201,response_body=jsonb_build_object('upload_id',p_upload),resource_id=p_upload WHERE tenant_id=sh.tenant_id AND id=idem.id;
 SELECT * INTO u FROM photo.upload_sessions WHERE tenant_id=sh.tenant_id AND upload_sessions.id=p_upload;
 RETURN QUERY SELECT 'created',u.id,u.project_id,u.collection_id,u.state,u.revision,u.filename,u.expected_size,u.actual_size,u.expires_at,u.object_key,u.multipart_id,u.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_portal_upload(bytea,bytea,text,text,uuid,uuid,text,text,bigint,text,text,text,text,timestamptz) TO photo_portal;

-- Read/attach always binds the upload to the current guest session and current
-- share policy. Internal storage identifiers are returned only to the server.
-- +goose StatementBegin
CREATE FUNCTION photo.get_portal_upload(p_session_hash bytea,p_upload uuid,p_for_update boolean DEFAULT false)
RETURNS TABLE(id uuid,project_id uuid,collection_id uuid,state text,revision bigint,filename text,expected_size bigint,actual_size bigint,
 expires_at timestamptz,object_key text,multipart_id text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;u photo.upload_sessions%ROWTYPE;
BEGIN
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,NULL,'upload');
 IF NOT FOUND THEN RETURN;END IF;
 IF p_for_update THEN SELECT * INTO u FROM photo.upload_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=p_upload FOR UPDATE;
 ELSE SELECT * INTO u FROM photo.upload_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=p_upload;END IF;
 IF NOT FOUND OR u.principal_type<>'guest' OR u.principal_id<>ident.session_id OR u.share_id<>ident.share_id OR u.policy_version<>ident.link_version OR u.collection_id<>ident.collection_id
 OR NOT EXISTS(SELECT FROM photo.share_sessions s JOIN photo.share_links l ON l.tenant_id=s.tenant_id AND l.id=s.share_id
  WHERE s.id=ident.session_id AND s.session_hash=p_session_hash AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()
  AND l.id=u.share_id AND l.policy_version=u.policy_version AND l.revoked_at IS NULL AND (l.expires_at IS NULL OR l.expires_at>clock_timestamp()) AND l.permissions->'upload'='true'::jsonb) THEN RETURN;END IF;
 RETURN QUERY SELECT u.id,u.project_id,u.collection_id,u.state,u.revision,u.filename,u.expected_size,u.actual_size,u.expires_at,u.object_key,u.multipart_id,u.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_portal_upload(bytea,uuid,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_portal_upload(bytea,uuid,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.get_portal_upload(bytea,uuid,boolean) TO photo_portal;

-- +goose StatementBegin
CREATE FUNCTION photo.attach_portal_multipart(p_session_hash bytea,p_csrf_hash bytea,p_upload uuid,p_multipart text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;u photo.upload_sessions%ROWTYPE;l photo.share_links%ROWTYPE;s photo.share_sessions%ROWTYPE;
BEGIN
 IF octet_length(p_csrf_hash)<>32 OR p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 OR p_multipart ~ '[\r\n]' THEN RETURN false;END IF;
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'upload');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO l FROM photo.share_links x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.share_id FOR UPDATE;
 SELECT * INTO s FROM photo.share_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=ident.session_id FOR UPDATE;
 IF l.id IS NULL OR s.id IS NULL OR s.session_hash<>p_session_hash OR s.csrf_hash<>p_csrf_hash OR s.revoked_at IS NOT NULL OR s.expires_at<=clock_timestamp()
 OR l.revoked_at IS NOT NULL OR (l.expires_at IS NOT NULL AND l.expires_at<=clock_timestamp()) OR l.policy_version<>s.link_version OR l.permissions->'upload'<>'true'::jsonb THEN RETURN false;END IF;
 SELECT * INTO u FROM photo.upload_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=p_upload FOR UPDATE;
 IF NOT FOUND OR u.principal_type<>'guest' OR u.principal_id<>ident.session_id OR u.share_id<>ident.share_id OR u.policy_version<>ident.link_version OR u.collection_id<>ident.collection_id THEN RETURN false;END IF;
 IF u.state='uploading' THEN RETURN u.multipart_id=p_multipart;END IF;
 IF u.state<>'initializing' OR u.multipart_id IS NOT NULL OR u.expires_at<=clock_timestamp() THEN RETURN false;END IF;
 UPDATE photo.upload_sessions SET multipart_id=p_multipart,state='uploading',revision=revision+1 WHERE tenant_id=u.tenant_id AND id=u.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.attach_portal_multipart(bytea,bytea,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.attach_portal_multipart(bytea,bytea,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.attach_portal_multipart(bytea,bytea,uuid,text) TO photo_portal;

-- +goose StatementBegin
CREATE FUNCTION photo.abort_portal_upload(p_session_hash bytea,p_csrf_hash bytea,p_upload uuid)
RETURNS TABLE(id uuid,project_id uuid,collection_id uuid,state text,revision bigint,filename text,expected_size bigint,actual_size bigint,
 expires_at timestamptz,object_key text,multipart_id text,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE ident record;hint photo.upload_sessions%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;l photo.share_links%ROWTYPE;s photo.share_sessions%ROWTYPE;
BEGIN
 SELECT * INTO ident FROM photo.authorize_portal_session(p_session_hash,p_csrf_hash,'upload');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO hint FROM photo.upload_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=p_upload;
 IF NOT FOUND OR hint.principal_type<>'guest' OR hint.principal_id<>ident.session_id OR hint.share_id<>ident.share_id THEN RETURN;END IF;
 IF hint.state='aborted' THEN RETURN QUERY SELECT hint.id,hint.project_id,hint.collection_id,hint.state,hint.revision,hint.filename,hint.expected_size,hint.actual_size,hint.expires_at,hint.object_key,hint.multipart_id,hint.media_type;RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=ident.tenant_id FOR UPDATE;
 SELECT * INTO l FROM photo.share_links WHERE tenant_id=ident.tenant_id AND id=ident.share_id FOR UPDATE;
 SELECT * INTO s FROM photo.share_sessions WHERE tenant_id=ident.tenant_id AND id=ident.session_id FOR UPDATE;
 SELECT * INTO u FROM photo.upload_sessions x WHERE x.tenant_id=ident.tenant_id AND x.id=p_upload FOR UPDATE;
 IF s.id IS NULL OR s.session_hash<>p_session_hash OR s.csrf_hash<>p_csrf_hash OR u.state NOT IN ('initializing','uploading')
 OR u.principal_type<>'guest' OR u.principal_id<>s.id OR u.share_id<>l.id OR u.policy_version<>s.link_version THEN RETURN;END IF;
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=u.tenant_id AND x.id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.state<>'reserved' OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.bytes<>u.expected_size OR q.files<>1
 OR l.reserved_bytes<q.bytes OR l.reserved_files<1 THEN RAISE EXCEPTION 'PORTAL_ABORT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=u.tenant_id AND reserved_bytes>=q.bytes AND reserved_files>=1;
 IF NOT FOUND THEN RAISE EXCEPTION 'PORTAL_ABORT_USAGE' USING ERRCODE='23514';END IF;
 UPDATE photo.share_links SET reserved_bytes=reserved_bytes-q.bytes,reserved_files=reserved_files-1 WHERE tenant_id=u.tenant_id AND id=l.id;
 UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=u.tenant_id AND id=q.id;
 UPDATE photo.upload_sessions SET state='aborted',revision=revision+1 WHERE tenant_id=u.tenant_id AND upload_sessions.id=u.id RETURNING * INTO u;
 RETURN QUERY SELECT u.id,u.project_id,u.collection_id,u.state,u.revision,u.filename,u.expected_size,u.actual_size,u.expires_at,u.object_key,u.multipart_id,u.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.abort_portal_upload(bytea,bytea,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.abort_portal_upload(bytea,bytea,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.abort_portal_upload(bytea,bytea,uuid) TO photo_portal;

-- Extend the generic expiry reaper with share reservation settlement. Lock
-- order remains usage -> share -> upload -> reservation, matching adoption.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.expire_upload_session() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.upload_sessions%ROWTYPE;u photo.upload_sessions%ROWTYPE;q photo.quota_reservations%ROWTYPE;l photo.share_links%ROWTYPE;
BEGIN
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'UPLOAD_EXPIRY_REQUIRES_READ_COMMITTED';END IF;
 SELECT x.* INTO hint FROM photo.upload_sessions x WHERE x.state IN ('initializing','uploading') AND x.expires_at<=clock_timestamp() ORDER BY x.expires_at,x.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF hint.share_id IS NOT NULL THEN SELECT * INTO l FROM photo.share_links WHERE tenant_id=hint.tenant_id AND id=hint.share_id FOR UPDATE;END IF;
 SELECT x.* INTO u FROM photo.upload_sessions x WHERE x.tenant_id=hint.tenant_id AND x.id=hint.id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND OR u.state NOT IN ('initializing','uploading') OR u.expires_at>clock_timestamp() OR u.share_id IS DISTINCT FROM l.id THEN RETURN false;END IF;
 SELECT x.* INTO q FROM photo.quota_reservations x WHERE x.tenant_id=u.tenant_id AND x.id=u.reservation_id FOR UPDATE;
 IF NOT FOUND OR q.holder_type<>'upload' OR q.holder_id<>u.id OR q.state<>'reserved' OR q.bytes<>u.expected_size OR q.files<>1 THEN RAISE EXCEPTION 'UPLOAD_EXPIRY_RESERVATION' USING ERRCODE='23514';END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=u.tenant_id AND reserved_bytes>=q.bytes AND reserved_files>=q.files;
 IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_EXPIRY_ACCOUNTING' USING ERRCODE='23514';END IF;
 IF l.id IS NOT NULL THEN
  UPDATE photo.share_links SET reserved_bytes=reserved_bytes-q.bytes,reserved_files=reserved_files-q.files
  WHERE tenant_id=u.tenant_id AND id=l.id AND reserved_bytes>=q.bytes AND reserved_files>=q.files;
  IF NOT FOUND THEN RAISE EXCEPTION 'UPLOAD_EXPIRY_SHARE_ACCOUNTING' USING ERRCODE='23514';END IF;
 END IF;
 UPDATE photo.quota_reservations SET state='released' WHERE tenant_id=u.tenant_id AND id=q.id;
 UPDATE photo.upload_sessions SET state='expired',revision=revision+1 WHERE tenant_id=u.tenant_id AND id=u.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.expire_upload_session() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.expire_upload_session() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.expire_upload_session() TO photo_maintenance;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
