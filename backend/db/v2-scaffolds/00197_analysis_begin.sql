-- First transaction of session admission. It creates a draft run and quota
-- reservation before any Params/InputSnapshot object publication. No task is
-- runnable until the later staged graph is registered and sealed.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v22_analysis_draft(
 p_tenant uuid,p_run uuid,p_workflow uuid,p_project uuid,p_snapshot uuid,
 p_session uuid,p_selection uuid,p_expected bigint,p_manual jsonb,
 p_actor uuid,p_auth_session uuid,p_policy_sha text,p_config_sha text,
 p_config jsonb,p_bytes bigint,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,run_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; existing photo.runs%ROWTYPE;
 frozen photo.snapshots%ROWTYPE; context jsonb; h bytea; expected_run uuid;
 expected_workflow uuid; expected_snapshot uuid; capacity numeric; used numeric;
BEGIN
 IF p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$'
  OR p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$'
  OR p_policy_sha IS NULL OR p_policy_sha !~ '^[0-9a-f]{64}$'
  OR p_config_sha IS NULL OR p_config_sha !~ '^[0-9a-f]{64}$'
  OR p_bytes IS NULL OR p_bytes NOT BETWEEN 1 AND 9007199254740991
  OR jsonb_typeof(p_config) IS DISTINCT FROM 'object'
  OR p_config->>'project_id' IS DISTINCT FROM p_project::text
  OR p_config->>'session_id' IS DISTINCT FROM p_session::text
  OR jsonb_typeof(p_config->'v22_session_plan') IS DISTINCT FROM 'object'
  OR ((p_config->'v22_session_plan')-'schema_version'-'item_count'
   -'snapshot_digest'-'policy'-'context_sha')<>'{}'::jsonb
  OR p_config->'v22_session_plan'->>'schema_version' IS DISTINCT FROM '1'
  OR coalesce(p_config->'v22_session_plan'->>'item_count','') !~ '^[1-9][0-9]{0,3}$'
  OR jsonb_typeof(p_config->'v22_session_plan'->'policy') IS DISTINCT FROM 'object'
  OR coalesce(p_config->'v22_session_plan'->>'context_sha','') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_config->'v22_session_plan'->>'snapshot_digest','') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V22_ANALYSIS_DRAFT_INVALID' USING ERRCODE='22023';
 END IF;
 h:=public.digest(uuid_send(p_tenant)||convert_to(
  'V2.Admit:'||p_actor::text||':'||p_key,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_run:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_run)||convert_to('workflow','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_workflow:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 h:=public.digest(uuid_send(expected_run)||convert_to('analysis.snapshot','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_snapshot:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_run IS DISTINCT FROM expected_run OR
  p_workflow IS DISTINCT FROM expected_workflow OR
  p_snapshot IS DISTINCT FROM expected_snapshot THEN
  RAISE EXCEPTION 'V22_ANALYSIS_ID_INVALID' USING ERRCODE='22023';
 END IF;
 IF NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_auth_session)
  OR NOT EXISTS(SELECT 1 FROM photo.shoot_sessions x WHERE x.tenant_id=p_tenant
   AND x.id=p_session AND x.project_id=p_project) THEN
  RAISE EXCEPTION 'V22_ANALYSIS_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,
  route,key,request_hash,expires_at)
 VALUES(p_tenant,'user',p_actor,'V2.Admit',p_key,p_request_sha,
  clock_timestamp()+interval '24 hours')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant
  AND i.principal_type='user' AND i.principal_id=p_actor
  AND i.route='V2.Admit' AND i.key=p_key FOR UPDATE;
 IF idem.request_hash<>p_request_sha THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::uuid; RETURN;
 END IF;
 IF idem.resource_id IS NOT NULL THEN
  SELECT * INTO existing FROM photo.runs r WHERE r.tenant_id=p_tenant
   AND r.id=idem.resource_id AND r.created_by=p_actor AND r.kind='v2';
  IF existing.id IS NULL OR existing.id<>p_run THEN
   RAISE EXCEPTION 'V22_ANALYSIS_IDEMPOTENCY_CORRUPT' USING ERRCODE='23514';
  END IF;
  IF idem.response_status=202 AND existing.graph_sealed THEN
   RETURN QUERY SELECT 'replayed'::text,existing.id; RETURN;
  END IF;
  IF idem.response_status IS NULL AND existing.status='draft'
   AND NOT existing.graph_sealed AND existing.input_snapshot_id=p_snapshot
   AND existing.config_hash=p_config_sha AND existing.config_snapshot=p_config
   AND EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
    AND w.run_id=p_run AND w.id=p_workflow AND w.project_id=p_project
    AND w.workflow_type='session' AND w.policy_sha=p_policy_sha)
   AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant
    AND q.holder_type='run' AND q.holder_id=p_run AND q.state='reserved'
    AND q.bytes=p_bytes AND q.expires_at>clock_timestamp()) THEN
   RETURN QUERY SELECT 'pending'::text,existing.id; RETURN;
  END IF;
  RETURN QUERY SELECT 'expired'::text,existing.id; RETURN;
 END IF;
 context:=photo.preflight_v22_analysis(p_tenant,p_session,p_expected,
  p_selection,p_manual,p_actor,p_auth_session);
 IF context->>'project_id' IS DISTINCT FROM p_project::text THEN
  RAISE EXCEPTION 'V22_ANALYSIS_PROJECT_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT * INTO frozen FROM photo.snapshots s WHERE s.tenant_id=p_tenant
  AND s.id=p_snapshot AND s.kind='v2' AND s.collection_id=(context->>'collection_id')::uuid
  AND s.principal_type='user' AND s.principal_id=p_actor
  AND s.item_count=(context->>'item_count')::bigint
  AND s.item_count=(p_config->'v22_session_plan'->>'item_count')::bigint
  AND s.digest=p_config->'v22_session_plan'->>'snapshot_digest'
  AND s.expires_at IS NULL FOR SHARE;
 IF frozen.id IS NULL THEN
  RAISE EXCEPTION 'V22_ANALYSIS_FROZEN_SNAPSHOT_INVALID' USING ERRCODE='23514';
 END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT (t.quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants t
  WHERE t.id=p_tenant AND t.status='active' FOR UPDATE;
 SELECT u.storage_bytes::numeric+u.reserved_bytes INTO used FROM photo.tenant_usage u
  WHERE u.tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR
  used+p_bytes>9007199254740991 THEN
  RAISE EXCEPTION 'V22_ANALYSIS_QUOTA' USING ERRCODE='23514';
 END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,
  config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
 VALUES(p_run,p_tenant,'v2','draft',p_snapshot,p_config,p_config_sha,
  p_actor,'fail_fast',false);
 INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,
  input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(p_workflow,p_tenant,p_project,p_run,p_snapshot,'session','v2.2',p_policy_sha);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,
  holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',p_actor,'run',p_run,p_bytes,0,
  clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes
  WHERE tenant_id=p_tenant;
 UPDATE photo.idempotency_records SET resource_id=p_run WHERE id=idem.id;
 RETURN QUERY SELECT 'created'::text,p_run;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v22_analysis_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,jsonb,uuid,uuid,text,text,jsonb,bigint,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v22_analysis_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,jsonb,uuid,uuid,text,text,jsonb,bigint,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v22_analysis_draft(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,jsonb,uuid,uuid,text,text,jsonb,bigint,text,text)
 TO photo_scheduler;
