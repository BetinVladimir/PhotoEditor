-- +goose Up
-- Retouch-specific dispatch gate; preserve all prior reservation/worker targeting.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_retouch_preview_dispatch_ready(p_task uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT COALESCE((SELECT t.operation_version='1' AND t.resource_class='cpu'
 AND t.state IN('ready','queued') AND NOT t.cancel_requested
 AND t.execution_profile->>'backend'='cpu'
 AND r.kind='preview' AND r.status IN('queued','running') AND r.graph_sealed AND NOT r.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.operation_registry op WHERE op.operation=t.operation
  AND op.version=t.operation_version AND op.resource_class='cpu' AND op.v2_enabled AND op.backends ? 'cpu')
 AND photo.semantic_retouch_preview_closure_ready(t.id) IS TRUE
 AND photo.semantic_retouch_preview_run_current(t.tenant_id,r.id) IS TRUE
 AND photo.v27_downstream_profile_ready(t.tenant_id,i.project_id,t.operation,t.operation_version,'cpu',
  r.v27_preapply_preview_profile) IS TRUE
 AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id
  AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved' AND q.principal_type='user'
  AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL
  AND q.bytes=(r.v27_preapply_preview_profile->'resource_estimate'->>'output_bytes')::bigint
  AND q.expires_at>clock_timestamp())
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.semantic_intents i ON i.tenant_id=t.tenant_id
  AND i.id::text=r.config_snapshot->'semantic_retouch_preview'->'params'->>'intent_id'
 WHERE t.id=p_task AND t.operation='semantic.preview_retouch_region'),false);
$$;
ALTER FUNCTION photo.semantic_retouch_preview_dispatch_ready(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_retouch_preview_dispatch_ready(uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal,photo_semantic_admission;
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)
 RENAME TO reserve_dispatch_before_retouch_preview;
REVOKE ALL ON FUNCTION photo.reserve_dispatch_before_retouch_preview(uuid,text,bigint,bigint,bigint,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_scheduler,photo_portal,photo_semantic_admission;
CREATE FUNCTION photo.reserve_dispatch(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,
 p_event uuid,p_correlation uuid,p_trace text) RETURNS TABLE(outcome text,command jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE own_task boolean;reserved record;
BEGIN
 SELECT t.operation='semantic.preview_retouch_region' INTO own_task FROM photo.tasks t WHERE t.id=p_task;
 IF own_task IS TRUE AND photo.semantic_retouch_preview_dispatch_ready(p_task) IS DISTINCT FROM true THEN
  RETURN QUERY SELECT 'stale'::text,NULL::jsonb;RETURN;
 END IF;
 SELECT * INTO reserved FROM photo.reserve_dispatch_before_retouch_preview(
  p_task,p_node,p_ram,p_vram,p_scratch,p_event,p_correlation,p_trace);
 IF own_task IS TRUE AND reserved.outcome='queued'
 AND photo.semantic_retouch_preview_dispatch_ready(p_task) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'SEMANTIC_RETOUCH_PREVIEW_DISPATCH_STALE' USING ERRCODE='40001';
 END IF;
 RETURN QUERY SELECT reserved.outcome,reserved.command;
END $$;
ALTER FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_semantic_admission;
GRANT EXECUTE ON FUNCTION photo.reserve_dispatch(uuid,text,bigint,bigint,bigint,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
