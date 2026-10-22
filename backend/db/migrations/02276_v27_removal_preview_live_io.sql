-- +goose Up
-- Repeat removal authorization during leased processing, retaining all prior IO gates.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_removal_preview_task_live(p_task uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN t.operation='semantic.preview_remove_object' THEN
 t.operation_version='1' AND t.resource_class='gpu' AND EXISTS(
 SELECT 1 FROM photo.runs r JOIN photo.semantic_intents i ON i.tenant_id=r.tenant_id
 AND i.id::text=r.config_snapshot->'semantic_removal_preview'->'params'->>'intent_id'
 WHERE r.tenant_id=t.tenant_id AND r.id=t.run_id AND r.kind='preview'
 AND r.status='running' AND r.graph_sealed AND NOT r.cancel_requested
 AND photo.semantic_removal_preview_closure_ready(t.id) IS TRUE
 AND photo.semantic_removal_preview_run_current(t.tenant_id,r.id) IS TRUE
 AND photo.v27_downstream_profile_ready(t.tenant_id,i.project_id,'semantic.preview_remove_object','1','gpu',
 r.v27_preapply_preview_profile) IS TRUE
 AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id
 AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user'
 AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL
 AND q.bytes=(r.v27_preapply_preview_profile->'resource_estimate'->>'output_bytes')::bigint
 AND q.expires_at>clock_timestamp())) ELSE true END
 FROM photo.tasks t WHERE t.id=p_task;
$$;
ALTER FUNCTION photo.semantic_removal_preview_task_live(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_removal_preview_task_live(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) RENAME TO gateway_input_before_removal_preview;
REVOKE ALL ON FUNCTION photo.gateway_input_before_removal_preview(uuid,integer,bigint,uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.gateway_input(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,p_artifact uuid)
RETURNS TABLE(artifact_id uuid,bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_removal_preview_task_live(p_task) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.gateway_input_before_removal_preview(p_task,p_attempt,p_fence,p_worker,p_certificate,p_artifact);
END $$;
ALTER FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal;
GRANT EXECUTE ON FUNCTION photo.gateway_input(uuid,integer,bigint,uuid,text,uuid) TO photo_gateway;
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) RENAME TO heartbeat_task_before_removal_preview;
REVOKE ALL ON FUNCTION photo.heartbeat_task_before_removal_preview(uuid,integer,bigint,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal;
CREATE FUNCTION photo.heartbeat_task(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_progress jsonb)
RETURNS TABLE(accepted boolean,lease_until timestamptz,cancel_requested boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_removal_preview_task_live(p_task) IS DISTINCT FROM true THEN
 RETURN QUERY SELECT false,NULL::timestamptz,true;RETURN;END IF;
 RETURN QUERY SELECT * FROM photo.heartbeat_task_before_removal_preview(p_task,p_attempt,p_fence,p_worker,p_progress);
END $$;
ALTER FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb)
 FROM PUBLIC,photo_api,photo_gateway,photo_scheduler,photo_portal;
GRANT EXECUTE ON FUNCTION photo.heartbeat_task(uuid,integer,bigint,uuid,jsonb) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
