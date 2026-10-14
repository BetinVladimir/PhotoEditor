-- +goose Up
-- Bind the caller's frozen request to generation admission and readback.
-- The existing five-argument functions remain as owner-only implementation
-- details. API callers must use the checked entry points.
-- +goose StatementBegin
CREATE FUNCTION photo.start_v30_archive_begin_generation_checked(
 p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid,p_request_sha text)
RETURNS TABLE(generation_id uuid,writer_token uuid,object_key text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 -- Preserve the tenant -> upload lock order of the underlying Start function.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.v30_archive_uploads
 WHERE tenant_id=p_tenant AND id=p_upload AND actor_id=p_actor AND token=p_reservation
 AND authority_session_id=p_session AND request_sha=p_request_sha FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.start_v30_archive_begin_generation(
  p_tenant,p_actor,p_session,p_upload,p_reservation);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_archive_begin_generation_checked(
 p_tenant uuid,p_actor uuid,p_session uuid,p_upload uuid,p_reservation uuid,p_request_sha text)
RETURNS TABLE(generation_id uuid,writer_token uuid,object_key text,state text,multipart_id text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM id FROM photo.v30_archive_uploads
 WHERE tenant_id=p_tenant AND id=p_upload AND actor_id=p_actor AND token=p_reservation
 AND authority_session_id=p_session AND request_sha=p_request_sha FOR UPDATE;
 IF NOT FOUND THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.read_v30_archive_begin_generation(
  p_tenant,p_actor,p_session,p_upload,p_reservation);
END $$;
-- +goose StatementEnd

ALTER FUNCTION photo.start_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.start_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.start_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
ALTER FUNCTION photo.read_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_archive_begin_generation_checked(uuid,uuid,uuid,uuid,uuid,text) TO photo_api;
REVOKE EXECUTE ON FUNCTION photo.start_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) FROM photo_api;
REVOKE EXECUTE ON FUNCTION photo.read_v30_archive_begin_generation(uuid,uuid,uuid,uuid,uuid) FROM photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
