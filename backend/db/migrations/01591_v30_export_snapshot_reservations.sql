-- +goose Up
CREATE TABLE photo.v30_export_snapshot_documents (
 tenant_id uuid NOT NULL REFERENCES photo.tenants(id),
 project_id uuid NOT NULL,
 actor_id uuid NOT NULL,
 key text NOT NULL CHECK(length(key) BETWEEN 1 AND 128 AND key ~ '^[!-~]+$'),
 snapshot_id uuid NOT NULL,
 artifact_id uuid NOT NULL,
 request_sha text NOT NULL CHECK(request_sha ~ '^[0-9a-f]{64}$'),
 request_body text NOT NULL CHECK(octet_length(request_body) BETWEEN 2 AND 1048576),
 document_body text NOT NULL CHECK(octet_length(document_body) BETWEEN 2 AND 8388608),
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 bytes bigint NOT NULL CHECK(bytes BETWEEN 2 AND 8388608),
 bucket text NOT NULL CHECK(length(bucket) BETWEEN 3 AND 63),
 token uuid NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN('pending','ready')),
 object_version text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,project_id,actor_id,key),
 UNIQUE(tenant_id,snapshot_id),UNIQUE(tenant_id,artifact_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES photo.projects(tenant_id,id),
 CHECK(state='pending' OR (object_version IS NOT NULL AND object_version NOT IN('','null') AND length(object_version)<=1024))
);
ALTER TABLE photo.v30_export_snapshot_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.v30_export_snapshot_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.v30_export_snapshot_documents TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.v30_export_snapshot_documents TO photo_fn_owner;
REVOKE ALL ON photo.v30_export_snapshot_documents FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_snapshot_scope(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_snapshot uuid,p_artifact uuid,p_request text,p_body text,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;body jsonb;node jsonb;ordinal bigint;binding jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects p JOIN photo.tenants t ON t.id=p.tenant_id WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL AND p.download_allowed AND t.status='active')
 OR p_snapshot IS NULL OR p_artifact IS DISTINCT FROM photo.v30_uuid5(p_snapshot,'interchange:export-snapshot:artifact:1')
 OR p_request IS NULL OR octet_length(p_request) NOT BETWEEN 2 AND 1048576 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 8388608
 OR p_sha IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex') THEN RETURN false;END IF;
 request:=p_request::jsonb;body:=p_body::jsonb;
 IF jsonb_typeof(request) IS DISTINCT FROM 'object' OR request- 'items'<>'{}'::jsonb
 OR jsonb_typeof(request->'items') IS DISTINCT FROM 'array' OR jsonb_array_length(request->'items') NOT BETWEEN 1 AND 5000
 OR body-ARRAY['schema_version','snapshot_id','snapshot_digest','items']<>'{}'::jsonb
 OR body->>'schema_version' IS DISTINCT FROM '1' OR body->>'snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR body->>'snapshot_digest' IS NULL OR body->>'snapshot_digest' !~ '^[0-9a-f]{64}$'
 OR body->'items' IS DISTINCT FROM request->'items' THEN RETURN false;END IF;
 IF (SELECT count(DISTINCT x#>>'{binding,asset_version_id}') FROM jsonb_array_elements(request->'items') x)<>jsonb_array_length(request->'items') THEN RETURN false;END IF;
 FOR node,ordinal IN SELECT value, ordinality-1 FROM jsonb_array_elements(request->'items') WITH ORDINALITY LOOP
  binding:=node->'binding';
  IF node->>'ordinal' IS DISTINCT FROM ordinal::text OR node->'working_image' IS DISTINCT FROM binding->'source'
  OR binding->>'canvas_hash' IS NULL OR binding->>'canvas_hash' !~ '^[0-9a-f]{64}$'
  OR photo.v26_render_recipe_scope(p_tenant,p_project,(binding->>'asset_version_id')::uuid,(node#>>'{recipe,artifact_id}')::uuid,node#>>'{recipe,sha256}',p_actor,p_session) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.master_artifact_id AND m.sha256=v.content_hash
   WHERE v.tenant_id=p_tenant AND v.id=(binding->>'asset_version_id')::uuid AND a.project_id=p_project AND a.deleted_at IS NULL AND a.upload_state='verified'
   AND v.review_state='accepted' AND m.id=(binding#>>'{source,artifact_id}')::uuid AND m.sha256=binding#>>'{source,sha256}'
   AND m.state='ready' AND m.verified_at IS NOT NULL AND m.output_role='working_image' AND m.media_type='application/json'
   AND m.object_version IS NOT NULL AND m.object_version NOT IN('','null') AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_snapshot_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_snapshot_scope(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v30_export_snapshot(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_key text,p_snapshot uuid,p_artifact uuid,p_request text,p_body text,p_sha text,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_export_snapshot_documents%ROWTYPE;capacity numeric;used numeric;fence uuid:=gen_random_uuid();size bigint;
BEGIN
 IF p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 128 OR p_key !~ '^[!-~]+$' OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63
 OR p_snapshot IS DISTINCT FROM photo.v30_uuid5(p_tenant,'interchange:export-snapshot:'||p_project||':'||p_actor||':'||p_key)
 OR photo.v30_export_snapshot_scope(p_tenant,p_actor,p_session,p_project,p_snapshot,p_artifact,p_request,p_body,p_sha) IS DISTINCT FROM true THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF capacity IS NULL THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 SELECT * INTO d FROM photo.v30_export_snapshot_documents x WHERE x.tenant_id=p_tenant AND x.project_id=p_project AND x.actor_id=p_actor AND x.key=p_key FOR UPDATE;
 IF FOUND THEN
  IF d.snapshot_id<>p_snapshot OR d.artifact_id<>p_artifact OR d.request_body<>p_request OR d.document_body<>p_body OR d.sha256<>p_sha OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  UPDATE photo.v30_export_snapshot_documents SET token=fence WHERE tenant_id=p_tenant AND snapshot_id=p_snapshot;
  RETURN QUERY SELECT 'resumed',p_artifact,fence,false;RETURN;
 END IF;
 size:=octet_length(p_body);
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF used IS NULL OR used+size>capacity OR used+size>9007199254740991 THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.v30_export_snapshot_documents(tenant_id,project_id,actor_id,key,snapshot_id,artifact_id,request_sha,request_body,document_body,sha256,bytes,bucket,token)
 VALUES(p_tenant,p_project,p_actor,p_key,p_snapshot,p_artifact,encode(sha256(convert_to(p_request,'UTF8')),'hex'),p_request,p_body,p_sha,size,p_bucket,fence);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',p_artifact,fence,false;
END $$;
ALTER FUNCTION photo.reserve_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.reserve_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.complete_v30_export_snapshot(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,p_key text,p_snapshot uuid,p_artifact uuid,p_token uuid,p_request_sha text,p_sha text,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_export_snapshot_documents%ROWTYPE;
BEGIN
 IF p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_export_snapshot_documents x WHERE x.tenant_id=p_tenant AND x.project_id=p_project AND x.actor_id=p_actor AND x.key=p_key FOR UPDATE;
 IF NOT FOUND OR d.snapshot_id IS DISTINCT FROM p_snapshot OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.request_sha IS DISTINCT FROM p_request_sha OR d.sha256 IS DISTINCT FROM p_sha OR d.bucket IS DISTINCT FROM p_bucket
 OR photo.v30_export_snapshot_scope(p_tenant,p_actor,p_session,p_project,p_snapshot,p_artifact,d.request_body,d.document_body,p_sha) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND a.bytes=d.bytes AND a.bucket=p_bucket AND a.object_version=p_version AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,'tenants/'||p_tenant||'/runs/'||p_snapshot||'/documents/'||p_artifact,p_version,p_sha,d.bytes,'application/json','ready',clock_timestamp(),'v30_export_snapshot');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'EXPORT_SNAPSHOT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v30_export_snapshot_documents SET state='ready',object_version=p_version WHERE tenant_id=p_tenant AND snapshot_id=p_snapshot;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text,text,text,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.complete_v30_export_snapshot(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,text,text,text,text) TO photo_api;
-- +goose StatementEnd
-- Retain current purge gates and allowlist; insert only this new tenant table.
-- +goose StatementBegin
DO $$ DECLARE definition text; BEGIN
 SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure) INTO definition;
 IF position('tables_to_purge constant text[]:=ARRAY[' IN definition)=0 THEN RAISE EXCEPTION 'PURGE_ALLOWLIST_NOT_FOUND';END IF;
 definition:=replace(definition,'tables_to_purge constant text[]:=ARRAY[','tables_to_purge constant text[]:=ARRAY[''v30_export_snapshot_documents'',');
 EXECUTE definition;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
