-- +goose Up
ALTER TABLE photo.runs ADD COLUMN v26_input_recovery_after timestamptz;
ALTER TABLE photo.runs ADD COLUMN v26_input_recovery_failures integer NOT NULL DEFAULT 0 CHECK(v26_input_recovery_failures>=0);
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v26_recovery_schedule() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF (NEW.v26_input_recovery_after IS NOT NULL OR NEW.v26_input_recovery_failures<>0) AND current_user<>'photo_fn_owner' THEN
   RAISE EXCEPTION 'V26_RECOVERY_SCHEDULE_PRIVATE' USING ERRCODE='42501';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.v26_input_recovery_after IS DISTINCT FROM OLD.v26_input_recovery_after
  OR NEW.v26_input_recovery_failures IS DISTINCT FROM OLD.v26_input_recovery_failures)
  AND current_user<>'photo_fn_owner' THEN
  RAISE EXCEPTION 'V26_RECOVERY_SCHEDULE_PRIVATE' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER v26_recovery_schedule_private BEFORE INSERT OR UPDATE OF v26_input_recovery_after,v26_input_recovery_failures ON photo.runs FOR EACH ROW EXECUTE FUNCTION photo.guard_v26_recovery_schedule();
REVOKE ALL ON FUNCTION photo.guard_v26_recovery_schedule() FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.record_v26_render_recovery(p_tenant uuid,p_batch uuid,p_failed boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; incomplete boolean;
BEGIN
 IF p_failed IS NULL THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 IF parent.id IS NULL OR parent.kind<>'bulk' OR parent.status<>'draft' OR parent.cancel_requested OR parent.graph_sealed
 OR parent.config_snapshot->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(parent.config_snapshot->'parts') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT EXISTS(SELECT 1 FROM jsonb_array_elements(parent.config_snapshot->'parts') part
 JOIN photo.runs child ON child.tenant_id=p_tenant AND child.id::text=part->>'run_id'
 JOIN photo.tasks task ON task.tenant_id=child.tenant_id AND task.run_id=child.id
 WHERE task.operation='render.compose' AND NOT photo.v2_input_closure_ready(task.id)) INTO incomplete;
 IF NOT p_failed AND incomplete THEN RETURN false;END IF;
 IF p_failed AND incomplete THEN
  UPDATE photo.runs SET v26_input_recovery_after=clock_timestamp()+make_interval(secs=>least(300,5*power(2,least(parent.v26_input_recovery_failures,6)))::integer),
   v26_input_recovery_failures=least(parent.v26_input_recovery_failures::bigint+1,2147483647)::integer WHERE id=p_batch AND tenant_id=p_tenant;
 ELSE
  UPDATE photo.runs SET v26_input_recovery_after=NULL,v26_input_recovery_failures=0 WHERE id=p_batch AND tenant_id=p_tenant;
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.record_v26_render_recovery(uuid,uuid,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.record_v26_render_recovery(uuid,uuid,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.record_v26_render_recovery(uuid,uuid,boolean) TO photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.next_v26_render_recovery(p_after uuid)
RETURNS TABLE(tenant_id uuid,batch_id uuid,project_id uuid,actor_id uuid,session_id uuid,auth_epoch bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.tenant_id,r.id,p.id,r.created_by,s.id,u.auth_epoch
 FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id::text=r.config_snapshot->>'project_id' AND p.archived_at IS NULL
 JOIN photo.users u ON u.id=r.created_by
 JOIN photo.sessions s ON s.user_id=u.id AND s.id::text=r.config_snapshot->>'auth_session_id' AND s.revoked_at IS NULL
 WHERE r.kind='bulk' AND r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed
 AND r.config_snapshot->'schema_version'='1'::jsonb
 AND (r.v26_input_recovery_after IS NULL OR r.v26_input_recovery_after<=clock_timestamp())
 AND r.config_snapshot ? 'parts' AND (p_after IS NULL OR r.id>p_after)
 AND EXISTS(
  SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(r.config_snapshot->'parts')='array' THEN r.config_snapshot->'parts' ELSE '[]'::jsonb END) part
  JOIN photo.runs child ON child.tenant_id=r.tenant_id AND child.id::text=part->>'run_id'
   AND child.kind='v2' AND child.status='draft' AND NOT child.cancel_requested AND NOT child.graph_sealed
   AND child.config_hash=part->>'config_sha'
  JOIN photo.tasks t ON t.tenant_id=child.tenant_id AND t.run_id=child.id
   AND t.operation='render.compose' AND t.operation_version='1' AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
  WHERE NOT photo.v2_input_closure_ready(t.id)
 )
 ORDER BY r.id LIMIT 1;
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
