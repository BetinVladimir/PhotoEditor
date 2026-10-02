-- +goose Up
-- Narrow recovery view of an unsealed immutable render draft; no table grants.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_render_draft_state(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(workflow_id uuid,project_id uuid,snapshot_id uuid,config jsonb,config_sha text,policy_sha text,reserved_bytes bigint)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.id,w.project_id,r.input_snapshot_id,r.config_snapshot,r.config_hash,w.policy_sha,q.bytes
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND tenant.status='active'
 AND r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.created_by=p_actor
 AND w.workflow_type='preview' AND w.renderer_version='v2.2'
 AND q.state='reserved' AND q.expires_at>clock_timestamp()
 AND r.config_snapshot->>'project_id'=w.project_id::text
 AND r.config_snapshot->>'input_snapshot_id'=r.input_snapshot_id::text
 AND r.config_snapshot->'v26_render'->>'auth_session_id'=p_session::text
 AND jsonb_typeof(r.config_snapshot->'v26_render'->'nodes')='array'
 AND photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session);
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_render_draft_state(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_draft_state(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_draft_state(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
