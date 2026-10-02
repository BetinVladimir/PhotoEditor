-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.begin_capture_draft(p_tenant uuid,p_job uuid,p_token uuid,p_run uuid,p_workflow uuid,
 p_config_jcs text,p_bytes bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE; s photo.snapshots%ROWTYPE;
 r photo.runs%ROWTYPE; config jsonb; config_sha text; policy_sha text; usage photo.tenant_usage%ROWTYPE; tenant photo.tenants%ROWTYPE;
BEGIN
 IF p_config_jcs IS NULL OR octet_length(p_config_jcs)>1048576 OR p_bytes NOT BETWEEN 1 AND 9007199254740991
  OR p_bytes IS NULL OR p_run IS NULL OR p_workflow IS NULL THEN RETURN false; END IF;
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO j FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'leased' OR j.lease_token IS DISTINCT FROM p_token OR j.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 SELECT * INTO s FROM photo.snapshots WHERE tenant_id=p_tenant AND id=j.capture_snapshot_id AND kind='v2' AND expires_at IS NULL;
 IF NOT FOUND OR s.item_count<>1 THEN RETURN false; END IF;
 config:=p_config_jcs::jsonb;config_sha:=encode(sha256(convert_to(p_config_jcs,'UTF8')),'hex');
 IF config->>'project_id' IS DISTINCT FROM j.project_id::text OR config->'v24_capture'->>'job_id' IS DISTINCT FROM j.id::text
  OR config->'v24_capture'->'policy' IS DISTINCT FROM j.capture_policy
  OR config->'v24_capture'->>'snapshot_digest' IS DISTINCT FROM s.digest
  OR config->'v24_capture'->'params'->'binding' IS DISTINCT FROM
   (SELECT payload FROM photo.snapshot_items WHERE tenant_id=p_tenant AND snapshot_id=s.id AND ordinal=0)
  OR config->'v24_capture'->'params'->'calibration' IS DISTINCT FROM j.capture_policy->'calibration'
  OR coalesce(config->'v24_capture'->>'policy_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
 policy_sha:=config->'v24_capture'->>'policy_sha';
 IF NOT EXISTS(SELECT 1 FROM photo.memberships m JOIN photo.projects p ON p.tenant_id=m.tenant_id AND p.id=j.project_id
  WHERE m.tenant_id=p_tenant AND m.user_id=s.principal_id AND m.status='active' AND m.role IN('owner','admin','editor')
   AND p.archived_at IS NULL AND (p.visibility<>'restricted' OR m.role IN('owner','admin') OR EXISTS(
    SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id AND acl.user_id=m.user_id AND acl.role='editor')))
 THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF FOUND THEN
  IF r.kind<>'v2' OR r.config_hash<>config_sha OR r.config_snapshot<>config OR r.input_snapshot_id<>s.id
   OR r.created_by<>s.principal_id OR j.preparation_workflow_id IS DISTINCT FROM p_workflow THEN
   RAISE EXCEPTION 'CAPTURE_DRAFT_CONFLICT' USING ERRCODE='23514'; END IF;
  RETURN r.status='draft' AND NOT r.cancel_requested;
 END IF;
 IF tenant.quotas->>'storage_bytes' IS NULL OR j.preparation_workflow_id IS NOT NULL OR usage.storage_bytes::numeric+usage.reserved_bytes+p_bytes>
  (tenant.quotas->>'storage_bytes')::numeric OR usage.reserved_bytes::numeric+p_bytes>9007199254740991 THEN RETURN false; END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(p_run,p_tenant,'v2','draft',s.id,config,config_sha,s.principal_id,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(p_workflow,p_tenant,j.project_id,p_run,s.id,'capture','v2.2',policy_sha);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',s.principal_id,'run',p_run,p_bytes,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 UPDATE photo.capture_assessment_jobs SET preparation_workflow_id=p_workflow WHERE tenant_id=p_tenant AND id=p_job;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_capture_draft(uuid,uuid,uuid,uuid,uuid,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_capture_draft(uuid,uuid,uuid,uuid,uuid,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_capture_draft(uuid,uuid,uuid,uuid,uuid,text,bigint) TO photo_scheduler;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.capture_preparation_progress(p_tenant uuid,p_job uuid,p_token uuid)
RETURNS TABLE(workflow_id uuid,run_id uuid,phase text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT w.id,r.id,CASE
  WHEN r.cancel_requested OR t.cancel_requested OR r.status IN('cancelling','cancelled') THEN 'cancelled'
  WHEN r.status='failed' OR t.state='failed' THEN 'failed'
  WHEN r.status='draft' THEN 'draft'
  WHEN EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt'
   AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id)) THEN 'ready'
  ELSE 'pending' END
 FROM photo.capture_assessment_jobs j JOIN photo.tenants tenant ON tenant.id=j.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=j.tenant_id AND w.project_id=j.project_id AND w.id=j.preparation_workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 LEFT JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation='capture.prepare' AND t.operation_version='1'
 WHERE j.tenant_id=p_tenant AND j.id=p_job AND j.state='leased' AND j.lease_token=p_token
  AND j.lease_until>clock_timestamp() AND w.workflow_type='capture' AND w.renderer_version='v2.2';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_preparation_progress(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_preparation_progress(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_preparation_progress(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
