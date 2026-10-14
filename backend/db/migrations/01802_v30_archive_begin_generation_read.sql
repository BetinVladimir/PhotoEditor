-- +goose Up
-- Read the one live generation for idempotent Begin retry. A 'creating'
-- generation is an unresolved Create, never permission to start another one.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_archive_begin_generation(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid)
RETURNS TABLE(generation_id uuid,writer_token uuid,object_key text,state text,multipart_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;g photo.v30_archive_begin_generations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.expires_at<=clock_timestamp()
 OR d.authority_session_id IS DISTINCT FROM p_session
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=d.project_id AND archived_at IS NULL AND download_allowed FOR SHARE;IF NOT FOUND THEN RETURN;END IF;
 SELECT q.* INTO g FROM photo.v30_archive_begin_generations q WHERE q.tenant_id=p_tenant AND q.upload_id=p_upload
 AND q.state IN('creating','created','aborting','bound') ORDER BY q.created_at,q.generation_id FOR UPDATE OF q LIMIT 1;
 IF NOT FOUND OR g.reservation_token IS DISTINCT FROM p_reservation OR g.creator_user_id IS DISTINCT FROM p_actor
 OR g.creator_session_id IS DISTINCT FROM p_session OR g.request_sha IS DISTINCT FROM d.request_sha THEN RETURN;END IF;
 IF g.state IN('created','bound') AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w
 WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.token=g.writer_token AND w.kind='begin'
 AND w.state='closed' AND w.receipt=g.multipart_id) THEN RETURN;END IF;
 IF g.state='creating' AND NOT EXISTS(SELECT 1 FROM photo.v30_archive_writers w
 WHERE w.tenant_id=p_tenant AND w.upload_id=p_upload AND w.token=g.writer_token AND w.kind='begin' AND w.state='open') THEN RETURN;END IF;
 RETURN QUERY SELECT g.generation_id,g.writer_token,g.object_key,g.state,g.multipart_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
