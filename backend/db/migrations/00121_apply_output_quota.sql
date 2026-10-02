-- +goose Up
-- Apply owns one conservative run reservation. Each task output atomically
-- consumes a slice of that reservation instead of reserving the same bytes a
-- second time. Completion/cleanup can keep using the common output accounting:
-- the slice remains in tenant_usage.reserved_bytes until it becomes storage or
-- the pending object is removed.
ALTER TABLE photo.output_uploads
 ADD COLUMN run_reservation_id uuid,
 ADD FOREIGN KEY(tenant_id,run_reservation_id) REFERENCES photo.quota_reservations(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.begin_output(p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_certificate text,
 p_key uuid,p_role text,p_bytes bigint,p_sha text,p_media text,p_bucket text)
RETURNS TABLE(outcome text,upload_id uuid,artifact_id uuid,object_key text,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; policy photo.operation_output_policies%ROWTYPE; old photo.output_uploads%ROWTYPE;
 q photo.quota_reservations%ROWTYPE; uid uuid:=gen_random_uuid(); aid uuid:=gen_random_uuid(); target text;
 maximum numeric; used numeric; budget numeric; from_run boolean:=false;
BEGIN
 IF p_key IS NULL OR p_key='00000000-0000-0000-0000-000000000000'::uuid OR p_role IS NULL OR p_role !~ '^[a-z][a-z0-9_-]{0,63}$'
 OR p_bytes IS NULL OR p_bytes<0 OR p_bytes>9007199254740991 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$'
 OR p_media IS NULL OR p_media !~ '^[a-z0-9.+-]+/[a-z0-9.+-]+$' OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN
 RAISE EXCEPTION 'INVALID_UPLOAD' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 IF t.id IS NULL OR NOT EXISTS(SELECT FROM photo.authorize_gateway_task(p_task,p_attempt,p_fence,p_worker,p_certificate)) THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 SELECT * INTO old FROM photo.output_uploads u WHERE u.tenant_id=t.tenant_id AND u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.idempotency_key=p_key FOR UPDATE;
 IF FOUND THEN
  IF old.worker_id<>p_worker OR old.certificate_sha256<>p_certificate OR old.role<>p_role OR old.bytes<>p_bytes OR old.sha256<>p_sha OR old.media_type<>p_media OR old.bucket<>p_bucket THEN
   RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,NULL::text,NULL::text;
  ELSIF old.state='ready' THEN RETURN QUERY SELECT 'replayed',old.id,old.artifact_id,old.object_key,old.sha256;
  ELSE RETURN QUERY SELECT 'busy',NULL::uuid,NULL::uuid,NULL::text,NULL::text;
  END IF; RETURN;
 END IF;
 SELECT * INTO policy FROM photo.operation_output_policies p WHERE p.operation=t.operation AND p.version=t.operation_version AND p.role=p_role;
 IF NOT FOUND OR NOT(policy.media_types ? p_media) OR p_bytes>policy.max_bytes THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 IF (SELECT count(*) FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence)>=128
 OR (SELECT count(*) FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.role=p_role)>=policy.max_count THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 budget:=(t.execution_profile->'resource_estimate'->>'output_bytes')::numeric;
 SELECT coalesce(sum(u.bytes),0) INTO used FROM photo.output_uploads u WHERE u.task_id=t.id AND u.attempt=p_attempt AND u.fence=p_fence;
 IF budget IS NULL OR budget<0 OR used+p_bytes>budget THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;

 -- The row lock serializes all output allocations for the run. Only apply runs
 -- may spend a run reservation; every other task follows the common path.
 SELECT x.* INTO q FROM photo.quota_reservations x JOIN photo.runs r ON r.tenant_id=x.tenant_id AND r.id=x.holder_id
 WHERE x.tenant_id=t.tenant_id AND x.holder_type='run' AND x.holder_id=t.run_id AND x.state='reserved'
 AND r.kind='apply' AND x.expires_at>clock_timestamp() FOR UPDATE OF x;
 IF FOUND AND q.bytes>=p_bytes THEN
  UPDATE photo.quota_reservations SET bytes=bytes-p_bytes WHERE tenant_id=q.tenant_id AND id=q.id AND bytes>=p_bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'RUN_RESERVATION_INCONSISTENT' USING ERRCODE='23514'; END IF;
  from_run:=true;
 ELSE
  SELECT (quotas->>'storage_bytes')::numeric INTO maximum FROM photo.tenants WHERE id=t.tenant_id;
  SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=t.tenant_id;
  IF maximum IS NULL OR used IS NULL OR used+p_bytes>maximum OR used+p_bytes>9007199254740991 THEN
   RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,NULL::text,NULL::text; RETURN; END IF;
 END IF;
 target:='tenants/'||t.tenant_id||'/tasks/'||t.id||'/attempts/'||p_attempt||'/fences/'||p_fence||'/'||aid;
 INSERT INTO photo.output_uploads(id,tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,run_reservation_id)
 VALUES(uid,t.tenant_id,t.id,p_attempt,p_fence,p_worker,p_certificate,p_key,aid,p_role,p_bucket,target,p_sha,p_bytes,p_media,CASE WHEN from_run THEN q.id END);
 IF NOT from_run THEN UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=t.tenant_id; END IF;
 RETURN QUERY SELECT 'created',uid,aid,target,p_sha;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text) OWNER TO photo_fn_owner;

-- +goose Down
-- Forward-only production migrations. Restore a backup for destructive rollback.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
