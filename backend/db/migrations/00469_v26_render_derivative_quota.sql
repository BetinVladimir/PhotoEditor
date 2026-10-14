-- +goose Up
-- Two existing preview profiles (52MiB each) plus bounded parameter documents (1MiB each).
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v26_render_derivative(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE; usage photo.tenant_usage%ROWTYPE; r photo.runs%ROWTYPE;
 source record; old photo.quota_reservations%ROWTYPE; budget constant bigint:=111149056;
BEGIN
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v26_derivative_task_id IS NULL OR r.kind<>'preview' OR r.status<>'draft'
  OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO source FROM photo.v26_render_derivative_source(p_tenant,r.v26_derivative_task_id);
 IF NOT FOUND OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM source.result_id::text
  OR r.config_snapshot->>'source_attempt' IS DISTINCT FROM source.attempt::text
  OR r.config_snapshot->>'source_fence' IS DISTINCT FROM source.fence::text
  OR r.config_snapshot->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',source.root_id,'sha256',source.root_sha) THEN RETURN false;END IF;
 -- Repeat after locking mutable source lifecycle/expiry fields.
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=source.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v26_derivative_task_id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.v26_render_derivative_source(p_tenant,r.v26_derivative_task_id) s
  WHERE s.result_id=source.result_id AND s.attempt=source.attempt AND s.fence=source.fence AND s.root_id=source.root_id AND s.root_sha=source.root_sha) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF FOUND THEN RETURN old.state='reserved' AND old.principal_type='user' AND old.principal_id=r.created_by
  AND old.bytes=budget AND old.files=0 AND old.expires_at>clock_timestamp();END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL OR usage.storage_bytes::numeric+usage.reserved_bytes+budget>(tenant.quotas->>'storage_bytes')::numeric
  OR usage.storage_bytes::numeric+usage.reserved_bytes+budget>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',r.created_by,'run',p_run,budget,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+budget WHERE tenant_id=p_tenant;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v26_render_derivative(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v26_render_derivative(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v26_render_derivative(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
