-- +goose Up
-- Private current-source observation for the four-sided preview draft.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_restoration_preview_intent_source(p_tenant uuid,p_run uuid)
RETURNS TABLE(project_id uuid,run_id uuid,workflow_id uuid,review_id uuid,
 result_id uuid,root_id uuid,root_sha text,attempt integer,fence bigint,
 actor_id uuid,auth_session_id uuid,source_version_id uuid,
 source_id uuid,source_sha text,canvas_hash text,candidate_id uuid,candidate_sha text)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT s.* FROM photo.runs r
 JOIN photo.runs parent ON parent.tenant_id=r.tenant_id
  AND parent.id::text=r.config_snapshot->>'source_run_id'
 CROSS JOIN LATERAL photo.v28_restoration_preview_source(r.tenant_id,r.v28_restoration_preview_task_id) s
 WHERE p_tenant IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='preview' AND r.status='draft'
  AND NOT r.graph_sealed AND NOT r.cancel_requested
  AND r.v28_restoration_preview_task_id IS NOT NULL
  AND r.config_snapshot->>'schema_version'='1'
  AND r.config_snapshot->>'source_run_id'=s.run_id::text
  AND r.config_snapshot->>'source_workflow_id'=s.workflow_id::text
  AND r.config_snapshot->>'source_task_id'=r.v28_restoration_preview_task_id::text
  AND r.config_snapshot->>'source_review_id'=s.review_id::text
  AND r.config_snapshot->>'source_result_id'=s.result_id::text
  AND r.config_snapshot->>'source_attempt'=s.attempt::text
  AND r.config_snapshot->>'source_fence'=s.fence::text
  AND r.config_snapshot->>'project_id'=s.project_id::text
  AND r.config_snapshot->>'actor_id'=s.actor_id::text AND r.created_by=s.actor_id
  AND r.config_snapshot->>'auth_session_id'=s.auth_session_id::text
  AND r.input_snapshot_id=parent.input_snapshot_id
  AND r.config_snapshot->>'source_version_id'=s.source_version_id::text
  AND r.config_snapshot->>'canvas_hash'=s.canvas_hash
  AND r.config_snapshot->'root'=jsonb_build_object('artifact_id',s.root_id,'sha256',s.root_sha)
  AND r.config_snapshot->'before'=jsonb_build_object('artifact_id',s.source_id,'sha256',s.source_sha)
  AND r.config_snapshot->'after'=jsonb_build_object('artifact_id',s.candidate_id,'sha256',s.candidate_sha)
  AND coalesce(r.config_snapshot->>'srgb_sha256','') ~ '^[0-9a-f]{64}$'
  AND r.config_snapshot->'sides'=jsonb_build_array('before','after')
  AND r.config_snapshot->'variants'=jsonb_build_array('preview1600','preview2560');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_restoration_preview_intent_source(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_restoration_preview_intent_source(uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_v28_restoration_preview(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE tenant photo.tenants%ROWTYPE; usage photo.tenant_usage%ROWTYPE;
 r photo.runs%ROWTYPE; s record; old photo.quota_reservations%ROWTYPE;
 budget constant bigint:=222298112;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL THEN RETURN false;END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v28_restoration_preview_task_id IS NULL OR r.kind<>'preview'
  OR r.status<>'draft' OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v28_restoration_preview_intent_source(p_tenant,p_run);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=s.project_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=s.run_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant
  AND id=r.v28_restoration_preview_task_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND id=s.review_id FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.root_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.source_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.candidate_id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v28_restoration_preview_intent_source(p_tenant,p_run) current_source
  WHERE current_source.review_id=s.review_id AND current_source.result_id=s.result_id
   AND current_source.root_id=s.root_id AND current_source.root_sha=s.root_sha
   AND current_source.source_id=s.source_id AND current_source.source_sha=s.source_sha
   AND current_source.candidate_id=s.candidate_id AND current_source.candidate_sha=s.candidate_sha
   AND current_source.attempt=s.attempt AND current_source.fence=s.fence) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF FOUND THEN RETURN old.state='reserved' AND old.principal_type='user'
  AND old.principal_id=r.created_by AND old.bytes=budget AND old.files=0
  AND old.expires_at>clock_timestamp();END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL OR
  usage.storage_bytes::numeric+usage.reserved_bytes+budget>
   (tenant.quotas->>'storage_bytes')::numeric OR
  usage.storage_bytes::numeric+usage.reserved_bytes+budget>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,
  holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',r.created_by,'run',p_run,budget,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+budget WHERE tenant_id=p_tenant;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_v28_restoration_preview(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_v28_restoration_preview(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.reserve_v28_restoration_preview(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_v28_restoration_preview_quota(p_tenant uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.v28_restoration_preview_task_id IS NULL OR r.kind<>'preview'
  OR r.status NOT IN('completed','completed_with_errors','failed','cancelled') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.state NOT IN('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT 1 FROM photo.output_uploads u JOIN photo.tasks t
  ON t.tenant_id=u.tenant_id AND t.id=u.task_id
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND u.state NOT IN('ready','cleaned'))
 OR EXISTS(SELECT 1 FROM photo.backend_documents d WHERE d.tenant_id=p_tenant
  AND d.run_id=p_run AND d.state NOT IN('ready','cleaned')) THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN true;END IF;
 IF q.state='released' AND q.bytes=0 THEN RETURN true;END IF;
 IF q.state<>'reserved' OR q.principal_type<>'user'
  OR q.principal_id<>r.created_by OR q.bytes<>222298112 THEN RETURN false;END IF;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes
  WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'RESTORATION_PREVIEW_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
 UPDATE photo.quota_reservations SET state='released',bytes=0,expires_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=q.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_v28_restoration_preview_quota(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_v28_restoration_preview_quota(uuid,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.release_v28_restoration_preview_quota(uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
