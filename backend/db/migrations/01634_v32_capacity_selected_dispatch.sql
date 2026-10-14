-- +goose Up
-- Select only on the physical advisory-lock session. Existing reservation
-- functions still establish resource/domain locks and authoritative dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.capacity_pick_selected_task(p_task uuid,p_tenant uuid,p_pool text,p_heavy_limit bigint)
RETURNS TABLE(task_id uuid,resource_class text,profile jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_task IS NULL OR p_tenant IS NULL OR p_heavy_limit IS NULL OR p_heavy_limit NOT BETWEEN 1 AND 9007199254740991 THEN
  RAISE EXCEPTION 'INVALID_CAPACITY_SELECTION' USING ERRCODE='22023'; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_catalog.pg_locks WHERE locktype='advisory'
  AND pid=pg_backend_pid() AND granted AND classid=2026::oid AND objid=32::oid AND objsubid=2
  AND mode='ExclusiveLock' AND database=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database())) THEN
  RAISE EXCEPTION 'CAPACITY_LEADER_REQUIRED' USING ERRCODE='55000'; END IF;
 INSERT INTO photo.scheduler_tenants(tenant_id) SELECT id FROM photo.tenants WHERE id=p_tenant AND status='active'
 ON CONFLICT(tenant_id) DO NOTHING;
 PERFORM tenant_id FROM photo.scheduler_tenants WHERE tenant_id=p_tenant FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN; END IF;
 RETURN QUERY SELECT t.id,t.resource_class,t.execution_profile FROM photo.tasks t
 JOIN photo.capacity_queue_candidates(p_pool) c ON c.task_id=t.id AND c.tenant_id=t.tenant_id
 WHERE t.id=p_task AND t.tenant_id=p_tenant AND (NOT c.heavy OR c.tenant_queued_heavy<p_heavy_limit);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capacity_pick_selected_task(uuid,uuid,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capacity_pick_selected_task(uuid,uuid,text,bigint) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.capacity_pick_selected_task(uuid,uuid,text,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
