-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_completion(p_tenant uuid,p_upload uuid,p_token uuid,p_bucket text,p_key text,p_version text,p_bytes bigint,p_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;
BEGIN
 IF p_token IS NULL OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.completion_token IS DISTINCT FROM p_token OR d.expected_bytes IS DISTINCT FROM p_bytes OR d.expected_sha IS DISTINCT FROM p_sha
 OR d.bucket IS DISTINCT FROM p_bucket OR d.object_key IS DISTINCT FROM p_key
 OR photo.v2_editor_authorized(p_tenant,d.project_id,d.actor_id,d.completion_session_id) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF d.state='verified' THEN RETURN d.object_version=p_version AND EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.sha256=p_sha AND a.bytes=p_bytes AND a.bucket=p_bucket AND a.object_key=p_key AND a.object_version=p_version AND a.media_type='application/zip'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='v30_interchange_archive' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'completing' OR d.completion_lease_until IS NULL OR d.completion_lease_until<=clock_timestamp() OR d.expires_at<=clock_timestamp() OR d.parts IS NULL THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,p_tenant,p_bucket,p_key,p_version,p_sha,p_bytes,'application/zip','ready',clock_timestamp(),'v30_interchange_archive');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'ARCHIVE_UPLOAD_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.v30_archive_uploads SET state='verified',object_version=p_version,completion_lease_until=NULL WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
ALTER FUNCTION photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_completion(uuid,uuid,uuid,text,text,text,bigint,text) TO photo_scheduler;
-- +goose StatementEnd
-- Read only through current editor authorization; ledger ownership alone never
-- grants access to another project. Parsing still validates physical ZIP layout.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_uploaded_archive_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_artifact uuid,p_sha text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT 1 FROM photo.v30_archive_uploads u JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 JOIN photo.projects p ON p.tenant_id=u.tenant_id AND p.id=u.project_id JOIN photo.tenants t ON t.id=p.tenant_id
 WHERE u.tenant_id=p_tenant AND u.project_id=p_project AND u.artifact_id=p_artifact AND u.expected_sha=p_sha AND u.state='verified'
 AND a.sha256=p_sha AND a.bytes=u.expected_bytes AND a.bucket=u.bucket AND a.object_key=u.object_key AND a.object_version=u.object_version
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/zip' AND a.output_role='v30_interchange_archive'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND p.archived_at IS NULL AND p.download_allowed AND t.status='active');
$$;
ALTER FUNCTION photo.v30_uploaded_archive_ready(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_uploaded_archive_ready(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_uploaded_archive_ready(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
