-- +goose Up
-- Discovery and retry recording share exact immutable planned-task identity.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_recovery_pending(p_tenant uuid,p_batch uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.runs parent
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(parent.config_snapshot->'parts')='array'
   THEN parent.config_snapshot->'parts' ELSE '[]'::jsonb END) part
  JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id::text=part->>'run_id'
   AND child.kind='v2' AND child.status='draft' AND NOT child.cancel_requested AND NOT child.graph_sealed
   AND child.created_by=parent.created_by AND child.input_snapshot_id=parent.input_snapshot_id
   AND child.config_hash=part->>'config_sha'
   AND child.config_snapshot->>'project_id'=parent.config_snapshot->>'project_id'
   AND child.config_snapshot->'v26_render'->>'auth_session_id'=parent.config_snapshot->>'auth_session_id'
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(child.config_snapshot->'v26_render'->'nodes')='array'
   THEN child.config_snapshot->'v26_render'->'nodes' ELSE '[]'::jsonb END) node
  JOIN photo.tasks task ON task.tenant_id=child.tenant_id AND task.run_id=child.id AND task.id::text=node->>'task_id'
   AND task.operation='render.compose' AND task.operation_version='1'
   AND task.params_artifact_id::text=node->'params'->>'artifact_id' AND task.input_digest=node->>'input_digest'
   AND task.state='blocked' AND task.attempt=0 AND NOT task.cancel_requested
  WHERE parent.tenant_id=p_tenant AND parent.id=p_batch
   AND parent.kind='bulk' AND parent.status='draft' AND NOT parent.cancel_requested AND NOT parent.graph_sealed
   AND parent.config_snapshot->'schema_version'='1'::jsonb
   AND NOT photo.v2_input_closure_ready(task.id)
 );
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_recovery_pending(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_recovery_pending(uuid,uuid) FROM PUBLIC;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.record_v26_render_recovery(p_tenant uuid,p_batch uuid,p_failed boolean) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE parent photo.runs%ROWTYPE; incomplete boolean;
BEGIN
 IF p_failed IS NULL THEN RETURN false;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=p_tenant AND id=p_batch FOR UPDATE;
 IF parent.id IS NULL OR parent.kind<>'bulk' OR parent.status<>'draft' OR parent.cancel_requested OR parent.graph_sealed
 OR parent.config_snapshot->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR jsonb_typeof(parent.config_snapshot->'parts') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 SELECT photo.v26_render_recovery_pending(p_tenant,p_batch) INTO incomplete;
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
 AND photo.v26_render_recovery_pending(r.tenant_id,r.id)
 ORDER BY r.id LIMIT 1;
$$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
