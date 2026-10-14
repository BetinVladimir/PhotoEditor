-- +goose Up
ALTER TABLE photo.v30_archive_uploads ADD COLUMN completion_session_id uuid;
CREATE INDEX ON photo.v30_archive_uploads(created_at,id) WHERE state='completing';
-- +goose StatementBegin
CREATE FUNCTION photo.queue_v30_archive_completion(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_request_sha text,p_size bigint,p_parts jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;part jsonb;ordinal bigint;total bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_parts IS NULL OR jsonb_typeof(p_parts) IS DISTINCT FROM 'array' OR octet_length(p_parts::text)>1048576 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.actor_id=p_actor FOR UPDATE;
 IF NOT FOUND OR d.request_sha IS DISTINCT FROM p_request_sha OR d.expected_bytes IS DISTINCT FROM p_size OR d.expires_at<=clock_timestamp()
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed) THEN RETURN false;END IF;
 total:=(d.expected_bytes+16777215)/16777216;
 IF jsonb_array_length(p_parts)<>total OR total NOT BETWEEN 1 AND 10000 THEN RETURN false;END IF;
 FOR part,ordinal IN SELECT value,ordinality FROM jsonb_array_elements(p_parts) WITH ORDINALITY LOOP
  IF jsonb_typeof(part) IS DISTINCT FROM 'object' OR part-ARRAY['part_no','etag']<>'{}'::jsonb
  OR part->>'part_no' IS DISTINCT FROM ordinal::text OR jsonb_typeof(part->'etag') IS DISTINCT FROM 'string'
  OR length(part->>'etag') NOT BETWEEN 1 AND 256 OR part->>'etag' ~ '[[:cntrl:]]' THEN RETURN false;END IF;
 END LOOP;
 IF d.state IN('completing','verified') THEN RETURN d.parts=p_parts;END IF;
 IF d.state<>'uploading' OR d.multipart_id IS NULL THEN RETURN false;END IF;
 UPDATE photo.v30_archive_uploads SET state='completing',parts=p_parts,completion_session_id=p_session,completion_token=NULL,completion_lease_until=NULL WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN true;
END $$;
ALTER FUNCTION photo.queue_v30_archive_completion(uuid,uuid,uuid,uuid,text,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.queue_v30_archive_completion(uuid,uuid,uuid,uuid,text,bigint,jsonb) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.queue_v30_archive_completion(uuid,uuid,uuid,uuid,text,bigint,jsonb) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.claim_v30_archive_completion() RETURNS SETOF photo.v30_archive_uploads
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;fence uuid:=gen_random_uuid();
BEGIN
 SELECT x.* INTO d FROM photo.v30_archive_uploads x JOIN photo.tenants t ON t.id=x.tenant_id JOIN photo.projects p ON p.tenant_id=x.tenant_id AND p.id=x.project_id
 WHERE x.state='completing' AND x.expires_at>clock_timestamp() AND (x.completion_lease_until IS NULL OR x.completion_lease_until<=clock_timestamp())
 AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed
 AND photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,x.completion_session_id)
 ORDER BY x.created_at,x.id FOR UPDATE OF x SKIP LOCKED LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.v30_archive_uploads SET completion_token=fence,completion_lease_until=clock_timestamp()+interval '2 minutes' WHERE tenant_id=d.tenant_id AND id=d.id RETURNING * INTO d;
 RETURN NEXT d;
END $$;
ALTER FUNCTION photo.claim_v30_archive_completion() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v30_archive_completion() FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.claim_v30_archive_completion() TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.renew_v30_archive_completion(p_tenant uuid,p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_uploads x SET completion_lease_until=clock_timestamp()+interval '2 minutes'
 WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.state='completing' AND x.completion_token=p_token AND x.completion_lease_until>clock_timestamp() AND x.expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM photo.tenants t JOIN photo.projects p ON p.tenant_id=t.id WHERE t.id=x.tenant_id AND p.id=x.project_id AND t.status='active' AND p.archived_at IS NULL AND p.download_allowed)
 AND photo.v2_editor_authorized(x.tenant_id,x.project_id,x.actor_id,x.completion_session_id);
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.renew_v30_archive_completion(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_v30_archive_completion(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.renew_v30_archive_completion(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.release_v30_archive_completion(p_tenant uuid,p_upload uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 UPDATE photo.v30_archive_uploads SET completion_token=NULL,completion_lease_until=NULL WHERE tenant_id=p_tenant AND id=p_upload AND state='completing' AND completion_token=p_token AND completion_lease_until>clock_timestamp();
 RETURN FOUND;
END $$;
ALTER FUNCTION photo.release_v30_archive_completion(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v30_archive_completion(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.release_v30_archive_completion(uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
