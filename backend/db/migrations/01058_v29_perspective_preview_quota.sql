-- +goose Up
-- Current saved intent observation, private to trusted publication functions.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_preview_intent_source(p_tenant uuid,p_run uuid)
RETURNS TABLE(project_id uuid,run_id uuid,workflow_id uuid,result_id uuid,
 root_id uuid,root_sha text,attempt integer,fence bigint,actor_id uuid,auth_session_id uuid,
 source_version_id uuid,source_id uuid,source_sha text,canvas_hash text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT s.* FROM photo.runs r
 CROSS JOIN LATERAL photo.v29_perspective_preview_source(r.tenant_id,r.v29_perspective_preview_task_id) s
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='preview' AND r.status='draft'
  AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.v29_perspective_preview_task_id IS NOT NULL
  AND r.config_snapshot->>'source_run_id'=s.run_id::text
  AND r.config_snapshot->>'source_workflow_id'=s.workflow_id::text
  AND r.config_snapshot->>'source_task_id'=r.v29_perspective_preview_task_id::text
  AND r.config_snapshot->>'source_result_id'=s.result_id::text
  AND r.config_snapshot->>'source_attempt'=s.attempt::text AND r.config_snapshot->>'source_fence'=s.fence::text
  AND r.config_snapshot->>'project_id'=s.project_id::text
  AND r.config_snapshot->>'actor_id'=s.actor_id::text AND r.created_by=s.actor_id
  AND r.config_snapshot->>'auth_session_id'=s.auth_session_id::text
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
  AND r.config_snapshot->'base'=jsonb_build_object('asset_version_id',s.source_version_id,
   'source',jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha),'canvas_hash',s.canvas_hash)
  AND coalesce(r.config_snapshot->>'srgb_sha256','') ~ '^[0-9a-f]{64}$'
  AND r.config_snapshot->'variants'=jsonb_build_array('preview1600','preview2560');
$$;
ALTER FUNCTION photo.v29_perspective_preview_intent_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_preview_intent_source(uuid,uuid) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v29_perspective_preview(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE; usage photo.tenant_usage%ROWTYPE; r photo.runs%ROWTYPE;
 source record; old photo.quota_reservations%ROWTYPE; budget constant bigint:=111149056;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v29_perspective_preview_task_id IS NULL OR r.kind<>'preview' OR r.status<>'draft'
  OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO source FROM photo.v29_perspective_preview_intent_source(p_tenant,p_run);
 IF NOT FOUND OR r.config_snapshot->>'source_result_id' IS DISTINCT FROM source.result_id::text
  OR r.config_snapshot->>'source_attempt' IS DISTINCT FROM source.attempt::text
  OR r.config_snapshot->>'source_fence' IS DISTINCT FROM source.fence::text
  OR r.config_snapshot->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',source.root_id,'sha256',source.root_sha) THEN RETURN false;END IF;
 -- Repeat after locking mutable source lifecycle/expiry fields.
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=source.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=source.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v29_perspective_preview_task_id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=source.root_id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.v29_perspective_preview_intent_source(p_tenant,p_run) s
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
ALTER FUNCTION photo.reserve_v29_perspective_preview(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v29_perspective_preview(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_v29_perspective_preview(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
