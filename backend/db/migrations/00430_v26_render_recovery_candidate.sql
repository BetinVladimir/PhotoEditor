-- +goose Up
-- Narrow scheduler-only discovery; scoped recovery repeats all authority.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v26_render_recovery(p_after uuid)
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
ALTER FUNCTION photo.next_v26_render_recovery(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v26_render_recovery(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.next_v26_render_recovery(uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
