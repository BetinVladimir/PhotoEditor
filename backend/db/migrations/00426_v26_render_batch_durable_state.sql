-- +goose Up
-- Durable aggregate phases support existing run status filters.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_batch_desired_state(p_tenant uuid,p_batch uuid) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo SET jit=off AS $$
 WITH parent AS MATERIALIZED (
 SELECT r.* FROM photo.runs r JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active' WHERE r.tenant_id=p_tenant AND r.id=p_batch
  AND r.kind='bulk' AND r.config_snapshot->'schema_version'='1'::jsonb
  AND r.config_snapshot ?& ARRAY['policy_sha','auth_session_id','parts']
  AND jsonb_typeof(r.config_snapshot->'parts')='array'
 ), parts AS MATERIALIZED (
 SELECT parent.status,parent.cancel_requested,child.status AS child_status,child.graph_sealed
 FROM parent CROSS JOIN LATERAL jsonb_array_elements(parent.config_snapshot->'parts') part
 LEFT JOIN photo.runs child ON child.tenant_id=parent.tenant_id AND child.id::text=part->>'run_id'
  AND child.kind='v2' AND child.created_by=parent.created_by AND child.input_snapshot_id=parent.input_snapshot_id
  AND child.config_hash=part->>'config_sha'
  AND child.config_snapshot->>'project_id'=parent.config_snapshot->>'project_id'
  AND child.config_snapshot->'v26_render'->>'auth_session_id'=parent.config_snapshot->>'auth_session_id'
 )
 SELECT CASE
  WHEN parent.status IN('completed','completed_with_errors','failed','cancelled','cancelling') THEN parent.status
  WHEN parent.cancel_requested THEN 'cancelling'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status IS NULL OR (child_status IN('draft','queued','running','waiting_review') AND NOT graph_sealed)) THEN 'draft'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='waiting_review' AND graph_sealed) THEN 'waiting_review'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='running' AND graph_sealed) THEN 'running'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='queued') THEN 'queued'
  WHEN EXISTS(SELECT 1 FROM parts WHERE child_status='cancelling') THEN 'running'
  WHEN EXISTS(SELECT 1 FROM parts) AND NOT EXISTS(SELECT 1 FROM parts WHERE child_status NOT IN('completed','completed_with_errors','failed','cancelled')) THEN 'running'
  ELSE 'draft' END
 FROM parent;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_batch_desired_state(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_batch_desired_state(uuid,uuid) FROM PUBLIC;
-- +goose StatementBegin
CREATE FUNCTION photo.sync_v26_render_batch_state() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo SET jit=off AS $$
DECLARE candidate photo.runs%ROWTYPE; parent photo.runs%ROWTYPE; desired text; cursor_value bigint;
BEGIN
 SELECT r.* INTO candidate FROM photo.runs r
 WHERE r.kind='bulk' AND r.status IN('draft','queued','running','waiting_review') AND NOT r.cancel_requested
 AND EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=r.tenant_id AND tenant.status='active')
 AND photo.v26_render_batch_desired_state(r.tenant_id,r.id) IS DISTINCT FROM r.status
 AND photo.v26_render_batch_desired_state(r.tenant_id,r.id) IN('draft','queued','running','waiting_review')
 ORDER BY r.created_at,r.id LIMIT 1;
 IF candidate.id IS NULL THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=candidate.tenant_id AND status='active' FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.id FOR UPDATE SKIP LOCKED;
 IF parent.id IS NULL OR parent.cancel_requested OR parent.status NOT IN('draft','queued','running','waiting_review') THEN RETURN false; END IF;
 desired:=photo.v26_render_batch_desired_state(parent.tenant_id,parent.id);
 IF desired IS NULL OR desired NOT IN('draft','queued','running','waiting_review') OR desired=parent.status THEN RETURN false; END IF;
 IF parent.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514'; END IF;
 INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(parent.tenant_id,0) ON CONFLICT(tenant_id) DO NOTHING;
 UPDATE photo.tenant_event_cursors SET last_cursor=last_cursor+1 WHERE tenant_id=parent.tenant_id AND last_cursor<9007199254740991 RETURNING last_cursor INTO cursor_value;
 IF NOT FOUND THEN RAISE EXCEPTION 'EVENT_CURSOR_EXHAUSTED' USING ERRCODE='23514'; END IF;
 UPDATE photo.runs SET status=desired,revision=revision+1 WHERE tenant_id=parent.tenant_id AND id=parent.id;
 INSERT INTO photo.run_events(tenant_id,cursor,run_id,principal_id,audience,type,payload)
 VALUES(parent.tenant_id,cursor_value,parent.id,parent.created_by,'principal','run.'||desired,
 jsonb_build_object('run_id',parent.id,'status',desired,'revision',parent.revision+1));
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.sync_v26_render_batch_state() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.sync_v26_render_batch_state() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.sync_v26_render_batch_state() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
