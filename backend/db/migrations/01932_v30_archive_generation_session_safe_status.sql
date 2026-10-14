-- +goose Up
-- Read-only generation state for a currently authorized session of the same
-- actor. It deliberately returns no writer or generation capability.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_archive_begin_generation_status(
 p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_upload uuid,
 p_reservation uuid,p_request_sha text)
RETURNS TABLE(state text,multipart_id text)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE g photo.v30_archive_begin_generations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 IF NOT EXISTS(
  SELECT 1 FROM photo.v30_archive_uploads u
  JOIN photo.tenants t ON t.id=u.tenant_id
  JOIN photo.projects p ON p.tenant_id=u.tenant_id AND p.id=u.project_id
  WHERE u.tenant_id=p_tenant AND u.project_id=p_project AND u.id=p_upload
   AND u.actor_id=p_actor AND u.token=p_reservation AND u.request_sha=p_request_sha
   AND u.expires_at>statement_timestamp() AND t.status='active'
   AND p.archived_at IS NULL AND p.download_allowed
   AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS TRUE
 ) THEN RETURN;END IF;
 SELECT q.* INTO g FROM photo.v30_archive_begin_generations q
 WHERE q.tenant_id=p_tenant AND q.upload_id=p_upload
  AND q.reservation_token=p_reservation AND q.creator_user_id=p_actor
  AND q.request_sha=p_request_sha
  AND q.state IN ('creating','created','aborting','bound')
 ORDER BY q.created_at,q.generation_id LIMIT 1;
 IF NOT FOUND THEN RETURN;END IF;
 IF g.state='creating' AND (g.multipart_id IS NOT NULL OR NOT EXISTS(
  SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant
   AND w.upload_id=p_upload AND w.token=g.writer_token AND w.kind='begin'
   AND w.state='open')) THEN RETURN;END IF;
 IF g.state IN ('created','bound') AND (g.multipart_id IS NULL OR NOT EXISTS(
  SELECT 1 FROM photo.v30_archive_writers w WHERE w.tenant_id=p_tenant
   AND w.upload_id=p_upload AND w.token=g.writer_token AND w.kind='begin'
   AND w.state='closed' AND w.receipt=g.multipart_id)) THEN RETURN;END IF;
 RETURN QUERY SELECT g.state,g.multipart_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_archive_begin_generation_status(uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_archive_begin_generation_status(uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_archive_begin_generation_status(uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
