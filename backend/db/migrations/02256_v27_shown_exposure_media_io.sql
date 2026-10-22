-- +goose Up
-- Source authorization remains live after claim, including each gateway grant.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_media_task_source_live(p_task uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN EXISTS(SELECT 1 FROM photo.media_retry_nodes WHERE id=p_task)
 THEN EXISTS(SELECT 1 FROM photo.media_retry_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id
  JOIN photo.media_retry_jobs j ON j.tenant_id=n.tenant_id AND j.event_id=n.event_id
  WHERE n.id=p_task AND j.run_id=n.run_id AND t.run_id=n.run_id AND t.inputs=n.inputs
  AND photo.semantic_shown_media_source_live(n.event_id,n.inputs) IS TRUE)
 ELSE true END;
$$;
ALTER FUNCTION photo.semantic_shown_media_task_source_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_media_task_source_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) RENAME TO gateway_input_pre_shown_media;
REVOKE ALL ON FUNCTION photo.gateway_input_pre_shown_media(uuid,integer,bigint,uuid,text,uuid)
 FROM PUBLIC,photo_gateway;
CREATE FUNCTION photo.gateway_input(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_artifact uuid)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_shown_media_task_source_live(p_task) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.gateway_input_pre_shown_media(p_task,p_attempt,p_fence,p_worker,p_certificate,p_artifact);
END $$;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) TO photo_gateway;
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) RENAME TO heartbeat_task_pre_shown_media;
REVOKE ALL ON FUNCTION photo.heartbeat_task_pre_shown_media(uuid,integer,bigint,uuid,jsonb) FROM PUBLIC,photo_worker;
CREATE FUNCTION photo.heartbeat_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_progress jsonb)
RETURNS TABLE(accepted boolean,lease_until timestamptz,cancel_requested boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_shown_media_task_source_live(p_task) IS DISTINCT FROM true THEN
  RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;
 END IF;
 RETURN QUERY SELECT * FROM photo.heartbeat_task_pre_shown_media(p_task,p_attempt,p_fence,p_worker,p_progress);
END $$;
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
