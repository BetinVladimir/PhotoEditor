-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_cancelled_training_repair_ready(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.runs r
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   AND w.input_snapshot_id=r.input_snapshot_id AND w.workflow_type='learning' AND w.renderer_version='v2.2'
  JOIN photo.learning_profiles p ON p.tenant_id=r.tenant_id AND p.project_id=w.project_id
   AND p.id::text=r.config_snapshot->'v23_training'->>'profile_id'
  JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
   AND s.kind='v2' AND s.digest=r.config_snapshot->'v23_training'->>'snapshot_digest'
  JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
   AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by
   AND q.files=0 AND q.share_id IS NULL
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='cancelled' AND r.cancel_requested AND r.graph_sealed
   AND jsonb_typeof(r.config_snapshot->'v23_training')='object'
   AND r.config_snapshot->>'project_id'=w.project_id::text
   AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id)
   AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id
    AND (t.operation NOT IN('learning.fit','learning.evaluate') OR t.operation_version<>'1'))
   AND NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
   AND NOT EXISTS(SELECT 1 FROM photo.graph_expansions e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.state<>'closed')
   AND NOT EXISTS(SELECT 1 FROM photo.reviews v WHERE v.tenant_id=r.tenant_id AND v.run_id=r.id AND v.state='pending')
   AND NOT EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
    WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id AND NOT (
     (u.state='ready' AND u.completed_at IS NOT NULL AND EXISTS(SELECT 1 FROM photo.artifacts a
      WHERE a.tenant_id=u.tenant_id AND a.id=u.artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL
      AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
      AND a.bucket=u.bucket AND a.object_key=u.object_key AND a.object_version=u.object_version
      AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.media_type=u.media_type AND a.output_role=u.role
      AND a.created_by_task_id=u.task_id AND a.produced_attempt=u.attempt AND a.produced_fence=u.fence))
     OR (u.state='cleaned' AND u.cleanup_token IS NULL AND u.cleanup_lease_until IS NULL
      AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=u.tenant_id AND a.id=u.artifact_id))))
   AND NOT EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=r.tenant_id AND d.run_id=r.id AND NOT (
     (d.state='ready' AND d.completed_at IS NOT NULL AND EXISTS(SELECT 1 FROM photo.artifacts a
      WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL
      AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
      AND a.bucket=d.bucket AND a.object_key='tenants/'||d.tenant_id::text||'/runs/'||d.run_id::text||'/documents/'||d.artifact_id::text
      AND a.object_version=d.object_version AND a.sha256=d.sha256 AND a.bytes=d.bytes
      AND a.media_type='application/json' AND a.output_role=CASE WHEN d.purpose IN('v23_frozen_base','v23_frozen_approved') THEN 'v23_frozen_recipe' ELSE d.purpose END))
     OR (d.state='cleaned' AND d.cleanup_token IS NULL AND d.cleanup_lease_until IS NULL
      AND NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id))))

 )
$$;
ALTER FUNCTION photo.v23_cancelled_training_repair_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_cancelled_training_repair_ready(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_maintenance,photo_gateway,photo_portal;
CREATE FUNCTION photo.release_v23_cancelled_training_quota(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 BEGIN
  PERFORM 1 FROM photo.tenants WHERE id=p_tenant FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN RETURN false;END IF;
  PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE SKIP LOCKED;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE SKIP LOCKED;
  IF NOT FOUND OR NOT photo.v23_cancelled_training_repair_ready(p_tenant,p_run) THEN RETURN false;END IF;
  PERFORM 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR SHARE NOWAIT;
  PERFORM 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND EXISTS(
   SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=u.task_id) ORDER BY u.id FOR UPDATE NOWAIT;
  PERFORM 1 FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE NOWAIT;
  PERFORM 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND (
   EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t ON t.tenant_id=u.tenant_id AND t.id=u.task_id
    WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND u.artifact_id=a.id)
   OR EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.artifact_id=a.id))
   ORDER BY a.id FOR SHARE NOWAIT;
  IF NOT photo.v23_cancelled_training_repair_ready(p_tenant,p_run) THEN RETURN false;END IF;
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'V23_TRAINING_HISTORICAL_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=q.id;
  RETURN true;
 EXCEPTION WHEN lock_not_available THEN RETURN false;
 END;
END $$;
ALTER FUNCTION photo.release_v23_cancelled_training_quota(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v23_cancelled_training_quota(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_maintenance,photo_gateway,photo_portal;
CREATE FUNCTION photo.release_next_v23_cancelled_training_quota() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;
BEGIN
 FOR candidate IN SELECT r.tenant_id,r.id FROM photo.runs r
  JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
  WHERE r.kind='v2' AND r.status='cancelled' AND r.cancel_requested AND r.graph_sealed
   AND q.state='reserved' AND q.principal_type='user' AND q.principal_id=r.created_by AND q.files=0 AND q.share_id IS NULL
   AND r.config_snapshot ? 'v23_training' AND photo.v23_cancelled_training_repair_ready(r.tenant_id,r.id)
  ORDER BY r.created_at,r.id LIMIT 64
 LOOP
  IF photo.release_v23_cancelled_training_quota(candidate.tenant_id,candidate.id) THEN RETURN true;END IF;
 END LOOP;
 RETURN false;
END $$;
ALTER FUNCTION photo.release_next_v23_cancelled_training_quota() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_next_v23_cancelled_training_quota() FROM PUBLIC,photo_api,photo_worker,photo_maintenance,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.release_next_v23_cancelled_training_quota() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
