-- +goose Up
-- Preview runs need their selected worker's V2 renderer independently of kind=v2.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_dispatch(p_task uuid,p_node text,p_ram bigint,p_vram bigint,p_scratch bigint,
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
 IF own_task IS TRUE AND reserved.outcome='queued' AND NOT EXISTS(
 SELECT 1 FROM photo.tasks t JOIN photo.worker_registrations w
 ON w.tenant_id=t.tenant_id AND w.worker_id=t.dispatch_worker_id
 WHERE t.id=p_task AND t.execution_profile->>'renderer_version'='v2.2'
 AND t.execution_profile->>'backend'='cpu' AND t.resource_class='cpu'
 AND w.node_id=p_node AND w.enabled AND w.db_principal IS NOT NULL
 AND w.v2_renderer_versions ? 'v2.2' AND w.resource_classes ? 'cpu'
 AND w.backends ? 'cpu' AND (w.qualified_operations->t.operation) ? 'cpu'
 AND w.last_seen>clock_timestamp()-interval '30 seconds') THEN
 RAISE EXCEPTION 'SEMANTIC_RETOUCH_PREVIEW_WORKER_STALE' USING ERRCODE='40001';
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
