-- +goose Up
-- API may call this only with its local transport's positive not-dispatched
-- proof. An S3 timeout after RoundTrip admission is never eligible.
-- +goose StatementBegin
CREATE FUNCTION photo.finish_v30_archive_begin_not_dispatched(p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid,p_generation uuid,p_writer uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.v30_archive_uploads%ROWTYPE;g photo.v30_archive_begin_generations%ROWTYPE;w photo.v30_archive_writers%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_generation IS NULL OR p_writer IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.v30_archive_uploads WHERE tenant_id=p_tenant AND id=p_upload FOR UPDATE;
 IF NOT FOUND OR d.actor_id IS DISTINCT FROM p_actor OR d.token IS DISTINCT FROM p_reservation OR d.state<>'reserved'
 OR d.multipart_id IS NOT NULL OR d.authority_session_id IS DISTINCT FROM p_session
 OR photo.v2_editor_authorized(p_tenant,d.project_id,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO g FROM photo.v30_archive_begin_generations WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation FOR UPDATE;
 IF NOT FOUND OR g.writer_token IS DISTINCT FROM p_writer OR g.reservation_token IS DISTINCT FROM p_reservation
 OR g.creator_user_id IS DISTINCT FROM p_actor OR g.creator_session_id IS DISTINCT FROM p_session OR g.request_sha IS DISTINCT FROM d.request_sha THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v30_archive_writers WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer FOR UPDATE;
 IF NOT FOUND OR w.kind<>'begin' THEN RETURN false;END IF;
 IF g.state='aborted' THEN RETURN g.multipart_id IS NULL AND g.terminal_evidence='not-dispatched'
 AND g.purged_at IS NOT NULL AND w.state='closed' AND w.receipt='not-dispatched';END IF;
 IF g.state<>'creating' OR g.multipart_id IS NOT NULL OR w.state<>'open' THEN RETURN false;END IF;
 UPDATE photo.v30_archive_writers SET state='closed',receipt='not-dispatched',closed_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND upload_id=p_upload AND token=p_writer;
 UPDATE photo.v30_archive_begin_generations SET state='aborted',terminal_evidence='not-dispatched',terminal_at=clock_timestamp(),
 purged_at=clock_timestamp(),revision=revision+1 WHERE tenant_id=p_tenant AND upload_id=p_upload AND generation_id=p_generation;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.finish_v30_archive_begin_not_dispatched(uuid,uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finish_v30_archive_begin_not_dispatched(uuid,uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.finish_v30_archive_begin_not_dispatched(uuid,uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
