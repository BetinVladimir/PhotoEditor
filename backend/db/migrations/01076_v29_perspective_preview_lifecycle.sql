-- +goose Up
-- Tenant-scoped lifecycle only. Never deletes objects or releases live writers' quota.
-- +goose StatementBegin
CREATE FUNCTION photo.maintain_v29_perspective_preview(p_tenant uuid,p_run uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;has_source boolean;has_quota boolean;any_reservation boolean;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN 'none';END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN 'none';END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN 'none';END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_perspective_preview_task_id IS NULL OR r.kind<>'preview' THEN RETURN 'none';END IF;
 IF r.status IN('completed','completed_with_errors','failed','cancelled') THEN
  -- Avoid reporting progress forever for an already released or absent reservation.
  IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant
   AND holder_type='run' AND holder_id=p_run AND state='reserved') THEN RETURN 'none';END IF;
  IF photo.release_v29_perspective_preview_quota(p_tenant,p_run) THEN RETURN 'released';END IF;
  RETURN 'none';
 END IF;
 IF r.status NOT IN('draft','running') OR r.cancel_requested THEN RETURN 'none';END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'project_id')::uuid FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'source_run_id')::uuid FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND (id=r.v29_perspective_preview_task_id OR run_id=p_run) ORDER BY id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(r.config_snapshot->'root'->>'artifact_id')::uuid FOR SHARE;
 PERFORM id FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 has_source:=EXISTS(SELECT 1 FROM photo.v29_perspective_preview_source(p_tenant,r.v29_perspective_preview_task_id) s
  WHERE s.project_id::text=r.config_snapshot->>'project_id'
   AND s.run_id::text=r.config_snapshot->>'source_run_id'
   AND s.workflow_id::text=r.config_snapshot->>'source_workflow_id'
   AND s.result_id::text=r.config_snapshot->>'source_result_id'
   AND s.attempt::text=r.config_snapshot->>'source_attempt' AND s.fence::text=r.config_snapshot->>'source_fence'
   AND jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)=r.config_snapshot->'root'
   AND s.actor_id::text=r.config_snapshot->>'actor_id' AND s.auth_session_id::text=r.config_snapshot->>'auth_session_id'
   AND s.source_version_id::text=r.config_snapshot->'base'->>'asset_version_id'
   AND jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)=r.config_snapshot->'base'->'source'
   AND s.canvas_hash=r.config_snapshot->'base'->>'canvas_hash');
 has_quota:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run AND state='reserved' AND expires_at>clock_timestamp()
  AND principal_type='user' AND principal_id=r.created_by);
 any_reservation:=EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run);
 IF NOT has_source OR (NOT has_quota AND (any_reservation OR r.status='running'
  OR r.created_at<=clock_timestamp()-interval '1 hour')) THEN
  UPDATE photo.runs SET status='cancelling',cancel_requested=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
  UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=p_tenant AND run_id=p_run
   AND state NOT IN('succeeded','failed','cancelled','skipped');
  RETURN 'cancel_requested'; -- request only; generic cancellation must settle workers before terminal release.
 END IF;
 IF r.status='running' AND r.graph_sealed
  AND (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)=2
  AND NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state NOT IN('succeeded','failed','cancelled','skipped'))
  AND EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND state IN('failed','cancelled','skipped')) THEN
  UPDATE photo.runs SET status='failed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
  RETURN 'failed';
 END IF;
 RETURN 'none';
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.maintain_v29_perspective_preview(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.maintain_v29_perspective_preview(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.maintain_v29_perspective_preview(uuid,uuid) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.pending_v29_perspective_preview_lifecycle(p_tenant uuid,p_after uuid)
RETURNS TABLE(run_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.id FROM photo.runs r WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.tenant_id=p_tenant AND r.v29_perspective_preview_task_id IS NOT NULL AND r.kind='preview'
  AND (p_after IS NULL OR r.id>p_after)
  AND ((r.status IN('draft','running') AND NOT r.cancel_requested)
   OR (r.status IN('completed','completed_with_errors','failed','cancelled') AND EXISTS(
    SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=r.id AND q.state='reserved')))
 ORDER BY r.id LIMIT 32;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_v29_perspective_preview_lifecycle(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_v29_perspective_preview_lifecycle(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.pending_v29_perspective_preview_lifecycle(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
