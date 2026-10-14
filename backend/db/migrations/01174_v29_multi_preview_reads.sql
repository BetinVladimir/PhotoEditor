-- +goose Up
-- Scheduler read adapters derive principal/project from the frozen producer, never caller-supplied authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_preview_read_object(p_tenant uuid,p_task uuid,p_operation text,p_attempt integer,p_fence bigint,p_root uuid,p_root_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s record;
BEGIN
 SELECT * INTO s FROM photo.v29_multi_preview_source(p_tenant,p_task,p_operation);
 IF NOT FOUND OR s.attempt IS DISTINCT FROM p_attempt OR s.fence IS DISTINCT FROM p_fence
 OR s.root_id IS DISTINCT FROM p_root OR s.root_sha IS DISTINCT FROM p_root_sha THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.v29_adopted_multi_object(p_tenant,s.project_id,p_task,s.actor_id,s.auth_session_id,p_operation,p_root,p_root_sha);
END $$;
ALTER FUNCTION photo.v29_multi_preview_read_object(uuid,uuid,text,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_preview_read_object(uuid,uuid,text,integer,bigint,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_multi_preview_read_object(uuid,uuid,text,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_preview_read_child(p_tenant uuid,p_task uuid,p_operation text,p_attempt integer,p_fence bigint,p_root uuid,p_root_sha text,p_child uuid,p_sha text,p_path text,p_contract text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s record;
BEGIN
 SELECT * INTO s FROM photo.v29_multi_preview_source(p_tenant,p_task,p_operation);
 IF NOT FOUND OR s.attempt IS DISTINCT FROM p_attempt OR s.fence IS DISTINCT FROM p_fence
 OR s.root_id IS DISTINCT FROM p_root OR s.root_sha IS DISTINCT FROM p_root_sha THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.v29_adopted_multi_child_object(p_tenant,s.project_id,p_task,s.actor_id,s.auth_session_id,p_operation,p_root,p_root_sha,p_child,p_sha,p_path,p_contract);
END $$;
ALTER FUNCTION photo.v29_multi_preview_read_child(uuid,uuid,text,integer,bigint,uuid,text,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_preview_read_child(uuid,uuid,text,integer,bigint,uuid,text,uuid,text,text,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_multi_preview_read_child(uuid,uuid,text,integer,bigint,uuid,text,uuid,text,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_preview_read_originals(p_tenant uuid,p_task uuid,p_operation text,p_attempt integer,p_fence bigint,p_root uuid,p_root_sha text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE s record;
BEGIN
 SELECT * INTO s FROM photo.v29_multi_preview_source(p_tenant,p_task,p_operation);
 IF NOT FOUND OR s.attempt IS DISTINCT FROM p_attempt OR s.fence IS DISTINCT FROM p_fence
 OR s.root_id IS DISTINCT FROM p_root OR s.root_sha IS DISTINCT FROM p_root_sha THEN RETURN NULL;END IF;
 RETURN photo.v29_adopted_multi_original_inventory(p_tenant,s.project_id,p_task,s.actor_id,s.auth_session_id,p_operation,p_root,p_root_sha);
END $$;
ALTER FUNCTION photo.v29_multi_preview_read_originals(uuid,uuid,text,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_preview_read_originals(uuid,uuid,text,integer,bigint,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_multi_preview_read_originals(uuid,uuid,text,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
