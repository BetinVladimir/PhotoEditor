-- +goose Up
-- Persist the maximum horizon BEFORE generating any presigned URL. API callers
-- must commit this authorization transaction before objectstore signing.
ALTER TABLE photo.v30_archive_uploads ADD COLUMN parts_authorized_until timestamptz;
-- +goose StatementBegin
CREATE FUNCTION photo.authorize_v30_archive_parts(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_token uuid,p_request_sha text,p_parts integer[])
RETURNS TABLE(bucket text,object_key text,multipart_id text,expected_bytes bigint,not_after timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;deadline timestamptz;total bigint;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_token IS NULL OR p_parts IS NULL OR cardinality(p_parts) NOT BETWEEN 1 AND 100 THEN RETURN;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p_parts) part WHERE part IS NULL)
 OR (SELECT count(DISTINCT part) FROM unnest(p_parts) part)<>cardinality(p_parts) THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN;END IF;
 SELECT x.* INTO d FROM photo.v30_archive_uploads x WHERE x.tenant_id=p_tenant AND x.id=p_upload AND x.actor_id=p_actor FOR UPDATE;
 IF NOT FOUND OR d.state<>'uploading' OR d.multipart_id IS NULL OR d.token IS DISTINCT FROM p_token OR d.request_sha IS DISTINCT FROM p_request_sha
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 total:=(d.expected_bytes+16777215)/16777216;
 IF EXISTS(SELECT 1 FROM unnest(p_parts) part WHERE part<1 OR part>total) THEN RETURN;END IF;
 deadline:=least(d.expires_at,clock_timestamp()+interval '15 minutes');
 IF deadline<clock_timestamp()+interval '61 seconds' THEN RETURN;END IF;
 UPDATE photo.v30_archive_uploads SET parts_authorized_until=greatest(coalesce(parts_authorized_until,deadline),deadline)
 WHERE tenant_id=p_tenant AND id=p_upload;
 RETURN QUERY SELECT d.bucket,d.object_key,d.multipart_id,d.expected_bytes,deadline;
END $$;
ALTER FUNCTION photo.authorize_v30_archive_parts(uuid,uuid,uuid,uuid,uuid,text,integer[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.authorize_v30_archive_parts(uuid,uuid,uuid,uuid,uuid,text,integer[]) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.authorize_v30_archive_parts(uuid,uuid,uuid,uuid,uuid,text,integer[]) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
