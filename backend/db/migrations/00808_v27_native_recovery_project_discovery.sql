-- +goose Up
-- Project discovery is a scheduling hint only; claim and full authority repeat.
-- +goose StatementBegin
CREATE FUNCTION photo.next_v27_native_parent_recovery_project(p_after uuid) RETURNS uuid
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT DISTINCT p.id FROM photo.runs r
 JOIN photo.tenants tenant ON tenant.id=r.tenant_id AND tenant.status='active'
 JOIN photo.projects p ON p.tenant_id=r.tenant_id AND p.id::text=r.config_snapshot->>'project_id' AND p.archived_at IS NULL
 JOIN photo.users u ON u.id=r.created_by
 JOIN photo.sessions s ON s.user_id=u.id AND s.id::text=r.config_snapshot->'v27_detector_prepare'->>'auth_session_id' AND s.revoked_at IS NULL
 WHERE r.kind='v2' AND r.v27_parent_recovery_enabled AND NOT r.v27_parent_recovery_done AND NOT r.cancel_requested
 AND jsonb_typeof(r.config_snapshot->'v27_detector_prepare'->'recovery'->'request_key')='string'
 AND length(r.config_snapshot->'v27_detector_prepare'->'recovery'->>'request_key') BETWEEN 1 AND 128
 AND (p_after IS NULL OR p.id>p_after)
 AND (r.v27_parent_recovery_after IS NULL OR r.v27_parent_recovery_after<=clock_timestamp())
 AND (r.v27_parent_recovery_until IS NULL OR r.v27_parent_recovery_until<=clock_timestamp())
 AND photo.v2_editor_authorized(r.tenant_id,p.id,r.created_by,s.id)
 AND EXISTS(SELECT 1 FROM photo.runs m WHERE m.tenant_id=r.tenant_id AND m.kind='v2' AND NOT m.cancel_requested
 AND m.config_snapshot->'v27_defect_native_prepare'->'parent_run_id'=r.config_snapshot->'v27_detector_prepare'->'parent_run_id'
 AND m.config_snapshot->'v27_defect_native_prepare'->'auth_session_id'=r.config_snapshot->'v27_detector_prepare'->'auth_session_id')
 ORDER BY p.id LIMIT 1;
$$;
ALTER FUNCTION photo.next_v27_native_parent_recovery_project(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.next_v27_native_parent_recovery_project(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.next_v27_native_parent_recovery_project(uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
