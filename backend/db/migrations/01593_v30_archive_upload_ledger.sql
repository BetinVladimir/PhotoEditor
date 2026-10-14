-- +goose Up
CREATE TABLE photo.v30_archive_uploads (
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 id uuid NOT NULL,
 project_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 key text NOT NULL CHECK(length(key) BETWEEN 1 AND 128 AND key ~ '^[!-~]+$'),
 request_body text NOT NULL CHECK(octet_length(request_body) BETWEEN 2 AND 16384),
 request_sha text NOT NULL CHECK(request_sha ~ '^[0-9a-f]{64}$'),
 filename text NOT NULL,
 expected_bytes bigint NOT NULL CHECK(expected_bytes BETWEEN 22 AND 68719476736),
 expected_sha text NOT NULL CHECK(expected_sha ~ '^[0-9a-f]{64}$'),
 artifact_id uuid NOT NULL,
 bucket text NOT NULL CHECK(length(bucket) BETWEEN 3 AND 63),
 object_key text NOT NULL,
 multipart_id text,
 token uuid NOT NULL,
 state text NOT NULL DEFAULT 'reserved' CHECK(state IN('reserved','uploading','completing','verified','aborting','aborted','failed')),
 parts jsonb,
 completion_token uuid,
 completion_lease_until timestamptz,
 object_version text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,actor_id,key),UNIQUE(tenant_id,artifact_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 CHECK(state NOT IN('uploading','completing','verified') OR multipart_id IS NOT NULL),
 CHECK(state<>'verified' OR (object_version IS NOT NULL AND object_version NOT IN('','null')))
);
ALTER TABLE photo.v30_archive_uploads ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_archive_uploads FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_archive_uploads TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v30_archive_uploads TO photo_fn_owner;
REVOKE ALL ON photo.v30_archive_uploads FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v30_archive_upload(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request text,p_bucket text)
RETURNS TABLE(outcome text,upload_id uuid,token uuid,state text,multipart_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE body jsonb;pid uuid;sid uuid;aid uuid;size bigint;sha text;name text;request_sha text;d photo.v30_archive_uploads%ROWTYPE;capacity numeric;used numeric;fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$'
 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 16384 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 body:=p_request::jsonb;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR body-ARRAY['project_id','filename','bytes','sha256']<>'{}'::jsonb
 OR NOT(body ?& ARRAY['project_id','filename','bytes','sha256']) THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 pid:=(body->>'project_id')::uuid;size:=(body->>'bytes')::bigint;sha:=body->>'sha256';name:=body->>'filename';request_sha:=encode(sha256(convert_to(p_request,'UTF8')),'hex');
 IF pid IS NULL OR size IS NULL OR size NOT BETWEEN 22 AND 68719476736 OR sha IS NULL OR sha !~ '^[0-9a-f]{64}$' OR name IS NULL
 OR length(name) NOT BETWEEN 5 AND 240 OR name ~ '[[:cntrl:]/\\:]' OR lower(right(name,4))<>'.zip'
 OR photo.v2_editor_authorized(p_tenant,pid,p_actor,p_session) IS DISTINCT FROM true THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=pid AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.actor_id=p_actor AND x.key=p_key FOR UPDATE;
 IF FOUND THEN
  IF d.request_sha<>request_sha OR d.request_body<>p_request OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
  IF d.expires_at<=clock_timestamp() OR d.state IN('aborting','aborted','failed') THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
  RETURN QUERY SELECT 'replayed',d.id,d.token,d.state,d.multipart_id;RETURN;
 END IF;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text;RETURN;END IF;
 sid:=photo.v30_uuid5(p_tenant,'interchange:archive-upload:'||p_actor||':'||p_key);aid:=photo.v30_uuid5(sid,'archive:artifact:1');
 INSERT INTO photo.v30_archive_uploads(tenant_id,id,project_id,actor_id,key,request_body,request_sha,filename,expected_bytes,expected_sha,artifact_id,bucket,object_key,token)
 VALUES(p_tenant,sid,pid,p_actor,p_key,p_request,request_sha,name,size,sha,aid,p_bucket,'tenants/'||p_tenant||'/interchange-uploads/'||sid||'/archive.zip',fence);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',sid,fence,'reserved',NULL::text;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN QUERY SELECT 'invalid',NULL::uuid,NULL::uuid,NULL::text,NULL::text;
END $$;
ALTER FUNCTION photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_archive_upload(uuid,uuid,uuid,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.bind_v30_archive_multipart(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_token uuid,p_request_sha text,p_multipart text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_multipart IS NULL OR length(p_multipart) NOT BETWEEN 1 AND 2048 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.actor_id=p_actor FOR UPDATE;
 IF NOT FOUND OR d.token IS DISTINCT FROM p_token OR d.request_sha IS DISTINCT FROM p_request_sha OR d.expires_at<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed) THEN RETURN false;END IF;
 IF d.state='uploading' THEN RETURN d.multipart_id=p_multipart;END IF;
 IF d.state<>'reserved' OR d.multipart_id IS NOT NULL THEN RETURN false;END IF;
 UPDATE photo.v30_archive_uploads SET multipart_id=p_multipart,state='uploading' WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
ALTER FUNCTION photo.bind_v30_archive_multipart(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.bind_v30_archive_multipart(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.bind_v30_archive_multipart(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE definition text; BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0 THEN RAISE EXCEPTION 'PURGE_ALLOWLIST_NOT_FOUND';END IF;
 EXECUTE replace(definition,'tables_to_purge constant text[]:=ARRAY[','tables_to_purge constant text[]:=ARRAY[''v30_archive_uploads'',');
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
