-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.stage_capture_preparation(p_tenant uuid,p_job uuid,p_token uuid,p_task uuid,
 p_params uuid,p_sha text,p_digest text,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.capture_assessment_jobs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 r photo.runs%ROWTYPE; old photo.tasks%ROWTYPE; expected uuid; h bytea;
BEGIN
 IF p_task IS NULL OR p_params IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_digest,'') !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false; END IF;
 PERFORM 1 FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO j FROM photo.capture_assessment_jobs WHERE tenant_id=p_tenant AND id=p_job FOR UPDATE;
 IF NOT FOUND OR j.state<>'leased' OR j.lease_token IS DISTINCT FROM p_token OR j.lease_until<=clock_timestamp() THEN RETURN false; END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=j.preparation_workflow_id;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
  OR w.workflow_type<>'capture' OR w.project_id<>j.project_id OR w.renderer_version<>'v2.2'
  OR p_profile IS DISTINCT FROM r.config_snapshot->'v24_capture'->'profile'
  OR p_sha IS DISTINCT FROM r.config_snapshot->'v24_capture'->>'params_sha'
  OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run'
   AND holder_id=r.id AND state='reserved' AND expires_at>clock_timestamp()) THEN RETURN false; END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('capture.prepare:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected<>p_task OR NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.object_version=d.object_version
  WHERE d.tenant_id=p_tenant AND d.run_id=r.id AND d.owner_id=j.id AND d.purpose='v24_capture_prepare_params'
   AND d.state='ready' AND d.artifact_id=p_params AND d.sha256=p_sha AND a.state='ready'
   AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
  OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=r.id AND id<>p_task)
  OR EXISTS(SELECT 1 FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=r.id) THEN RETURN false; END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
 IF FOUND THEN
  RETURN old.run_id=r.id AND old.operation='capture.prepare' AND old.operation_version='1'
   AND old.node_key='capture.prepare:0' AND old.params_artifact_id=p_params AND old.input_digest=p_digest
   AND old.execution_profile=p_profile AND old.resource_class='gpu' AND old.state='blocked' AND old.attempt=0 AND NOT old.cancel_requested;
 END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(p_task,p_tenant,r.id,'capture.prepare:0','capture.prepare','1','gpu','[]'::jsonb,p_params,p_digest,p_profile);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_capture_preparation(uuid,uuid,uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_capture_preparation(uuid,uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_capture_preparation(uuid,uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.capture_draft_config(p_tenant uuid,p_job uuid,p_token uuid)
RETURNS TABLE(config jsonb,config_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.config_snapshot,r.config_hash FROM photo.capture_assessment_jobs j
 JOIN photo.tenants tenant ON tenant.id=j.tenant_id AND tenant.status='active'
 JOIN photo.v2_workflows w ON w.tenant_id=j.tenant_id AND w.project_id=j.project_id AND w.id=j.preparation_workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 WHERE j.tenant_id=p_tenant AND j.id=p_job AND j.state='leased' AND j.lease_token=p_token
 AND j.lease_until>clock_timestamp() AND r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
 AND w.workflow_type='capture' AND w.renderer_version='v2.2';
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_draft_config(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_draft_config(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_draft_config(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
