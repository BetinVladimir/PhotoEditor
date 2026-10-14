-- +goose Up
-- These entry points are not yet used by production. They establish the
-- durable Create receipt boundary without treating S3 listing as a receipt.
-- +goose StatementBegin
CREATE FUNCTION photo.start_v30_archive_begin_generation(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid)
RETURNS TABLE(generation_id uuid,writer_token uuid,object_key text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE; gid uuid:=gen_random_uuid();ticket uuid:=gen_random_uuid();new_key text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.state<>'reserved'
 OR d.multipart_id IS NOT NULL OR d.expires_at<=clock_timestamp() OR d.authority_session_id IS DISTINCT FROM p_session
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 IF EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.state='open')
 OR EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.kind='begin'
   AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=w.tenant_id AND g.upload_id=w.upload_id AND g.writer_token=w.token))
 OR EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload AND g.state IN('creating','created','aborting','bound'))
 THEN RETURN;END IF;
 new_key:='tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/generations/'||gid||'/archive.zip';
 INSERT INTO photo.v30_archive_writers(tenant_id,upload_id,token,kind) VALUES(p_tenant,p_upload,ticket,'begin');
 INSERT INTO photo.v30_archive_begin_generations(tenant_id,upload_id,generation_id,writer_token,reservation_token,creator_user_id,creator_session_id,request_sha,object_key)
 VALUES(p_tenant,p_upload,gid,ticket,p_reservation,p_actor,p_session,d.request_sha,new_key);
 RETURN QUERY SELECT gid,ticket,new_key;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_begin_generation(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid,p_generation uuid,p_writer uuid,p_multipart text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;g photo.v30_archive_begin_generations%ROWTYPE;w photo.v30_archive_writers%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_generation IS NULL OR p_writer IS NULL
 OR p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 OR p_multipart ~ '[[:cntrl:]]' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.state<>'reserved'
 OR d.expires_at<=clock_timestamp() OR d.authority_session_id IS DISTINCT FROM p_session
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO g FROM photo.v30_archive_begin_generations WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation FOR UPDATE;
 IF NOT FOUND OR g.writer_token IS DISTINCT FROM p_writer OR g.reservation_token IS DISTINCT FROM p_reservation
 OR g.creator_user_id IS DISTINCT FROM p_actor OR g.creator_session_id IS DISTINCT FROM p_session OR g.request_sha IS DISTINCT FROM d.request_sha THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer FOR UPDATE;
 IF NOT FOUND OR w.kind<>'begin' THEN RETURN false;END IF;
 IF g.state IN('created','bound') THEN RETURN g.multipart_id=p_multipart AND w.state='closed' AND w.receipt=p_multipart;END IF;
 IF g.state<>'creating' OR w.state<>'open' THEN RETURN false;END IF;
 UPDATE photo.v30_archive_writers SET state='closed',receipt=p_multipart,closed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer;
 UPDATE photo.v30_archive_begin_generations SET state='created',multipart_id=p_multipart,terminal_evidence='s3-create-receipt',terminal_at=clock_timestamp(),revision=revision+1
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation;
 RETURN true;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.bind_v30_archive_begin_generation(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid,p_request_sha text,p_generation uuid,p_writer uuid,p_multipart text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;g photo.v30_archive_begin_generations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_generation IS NULL OR p_writer IS NULL
 OR p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 1024 OR p_multipart ~ '[[:cntrl:]]' THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.request_sha IS DISTINCT FROM p_request_sha
 OR d.expires_at<=clock_timestamp() OR d.authority_session_id IS DISTINCT FROM p_session
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO g FROM photo.v30_archive_begin_generations WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation FOR UPDATE;
 IF NOT FOUND OR g.writer_token IS DISTINCT FROM p_writer OR g.reservation_token IS DISTINCT FROM p_reservation
 OR g.creator_user_id IS DISTINCT FROM p_actor OR g.creator_session_id IS DISTINCT FROM p_session OR g.request_sha IS DISTINCT FROM p_request_sha
 OR g.multipart_id IS DISTINCT FROM p_multipart OR g.object_key IS DISTINCT FROM 'tenants/'||p_tenant||'/interchange-uploads/'||p_upload||'/generations/'||p_generation||'/archive.zip'
 OR NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.token=p_writer AND w.kind='begin' AND w.state='closed' AND w.receipt=p_multipart)
 THEN RETURN false;END IF;
 IF d.state='uploading' THEN RETURN g.state='bound' AND d.multipart_id=p_multipart AND d.object_key=g.object_key;END IF;
 IF d.state<>'reserved' OR d.multipart_id IS NOT NULL OR g.state<>'created' THEN RETURN false;END IF;
 UPDATE photo.v30_archive_uploads SET state='uploading',multipart_id=p_multipart,object_key=g.object_key,authority_session_id=p_session
 WHERE tenant_id=p_tenant AND id=p_upload;
 UPDATE photo.v30_archive_begin_generations SET state='bound',revision=revision+1
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation;
 RETURN true;
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) TO photo_api;
ALTER FUNCTION photo.finish_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.bind_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.bind_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) TO photo_api;

-- Legacy bind/recovery must never attach a generation's multipart to its old
-- object key. Exact-definition guards make migration fail if their bodies drift.
-- +goose StatementBegin
DO $$ DECLARE definition text;signature text;needle text;
BEGIN
 FOREACH signature IN ARRAY ARRAY[
  'photo.recover_v30_archive_begin(uuid,uuid,uuid,uuid,uuid)',
  'photo.bind_v30_archive_multipart(uuid,uuid,uuid,uuid,uuid,text,text)'
 ] LOOP
  SELECT pg_get_functiondef(signature::regprocedure) INTO definition;
  needle:=' IF d.state=''uploading'' THEN';
  IF definition IS NULL OR position(needle IN definition)=0 OR position('v30_archive_begin_generations' IN definition)>0
  THEN RAISE EXCEPTION 'ARCHIVE_LEGACY_GENERATION_GUARD_SHAPE: %',signature;END IF;
  EXECUTE replace(definition,needle,
   ' IF EXISTS(SELECT 1 FROM photo.v30_archive_begin_generations g WHERE g.tenant_id=p_tenant AND g.upload_id=p_upload) THEN RETURN '||
   CASE WHEN signature LIKE '%recover%' THEN 'NULL' ELSE 'false' END||';END IF;'||needle);
 END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
