-- Pending idempotency is committed with the draft shell. A replay resumes the
-- same run after transport publication crashes; only graph seal completes the
-- saved 202 JobTicket. Different request bytes under the same key conflict.
-- +goose StatementBegin
CREATE FUNCTION photo.begin_v2_admission(
 p_tenant uuid,p_run uuid,p_workflow_id uuid,p_project uuid,p_snapshot uuid,p_actor uuid,
 p_workflow text,p_policy_sha text,p_config_sha text,p_config jsonb,
 p_tasks jsonb,p_edges jsonb,p_bytes bigint,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,run_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; existing photo.runs%ROWTYPE;
 created boolean;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_actor IS NULL OR
  p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$' OR
  p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V2_IDEMPOTENCY_INVALID' USING ERRCODE='22023';
 END IF;
 INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,
  route,key,request_hash,expires_at)
 VALUES(p_tenant,'user',p_actor,'V2.Admit',p_key,p_request_sha,
  clock_timestamp()+interval '24 hours')
 ON CONFLICT(tenant_id,principal_type,principal_id,route,key) DO NOTHING;
 SELECT * INTO idem FROM photo.idempotency_records x WHERE x.tenant_id=p_tenant
  AND x.principal_type='user' AND x.principal_id=p_actor
  AND x.route='V2.Admit' AND x.key=p_key FOR UPDATE;
 IF idem.id IS NULL THEN RAISE EXCEPTION 'V2_IDEMPOTENCY_MISSING' USING ERRCODE='23514'; END IF;
 IF idem.request_hash<>p_request_sha THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::uuid; RETURN;
 END IF;
 IF idem.resource_id IS NOT NULL THEN
  SELECT * INTO existing FROM photo.runs r WHERE r.tenant_id=p_tenant
   AND r.id=idem.resource_id AND r.created_by=p_actor AND r.kind='v2';
  IF existing.id IS NULL THEN RAISE EXCEPTION 'V2_IDEMPOTENCY_CORRUPT' USING ERRCODE='23514'; END IF;
  IF idem.response_status=202 AND existing.graph_sealed THEN
   RETURN QUERY SELECT 'replayed'::text,existing.id; RETURN;
  END IF;
  IF idem.response_status IS NULL AND existing.status='draft' AND NOT existing.graph_sealed
   AND EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant
    AND q.holder_type='run' AND q.holder_id=existing.id AND q.state='reserved'
    AND q.expires_at>clock_timestamp()) THEN
   IF NOT photo.v2_pending_shell_matches(p_tenant,existing.id,p_workflow_id,
    p_project,p_snapshot,p_actor,p_workflow,p_policy_sha,p_config_sha,p_config,
    p_tasks,p_edges,p_bytes) THEN
    RETURN QUERY SELECT 'conflict'::text,existing.id; RETURN;
   END IF;
   RETURN QUERY SELECT 'pending'::text,existing.id; RETURN;
  END IF;
  RETURN QUERY SELECT 'expired'::text,existing.id; RETURN;
 END IF;
 SELECT photo.create_v2_draft_shell(p_tenant,p_run,p_workflow_id,p_project,p_snapshot,p_actor,
  p_workflow,p_policy_sha,p_config_sha,p_config,p_tasks,p_edges,p_bytes) INTO created;
 IF NOT created THEN RAISE EXCEPTION 'V2_DRAFT_REJECTED' USING ERRCODE='23514'; END IF;
 UPDATE photo.idempotency_records SET resource_id=p_run WHERE tenant_id=p_tenant AND id=idem.id;
 RETURN QUERY SELECT 'created'::text,p_run;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_v2_admission(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_v2_admission(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.begin_v2_admission(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint,text,text) TO photo_scheduler;

-- Read only the completed original ticket, never a draft placeholder.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_admission_ticket(p_tenant uuid,p_actor uuid,p_key text,p_request_sha text)
RETURNS jsonb LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT i.response_body FROM photo.idempotency_records i
 JOIN photo.runs r ON r.tenant_id=i.tenant_id AND r.id=i.resource_id
 WHERE i.tenant_id=p_tenant AND i.principal_type='user' AND i.principal_id=p_actor
  AND i.route='V2.Admit' AND i.key=p_key AND i.request_hash=p_request_sha
  AND i.response_status=202 AND r.kind='v2' AND r.graph_sealed
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_admission_ticket(uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_admission_ticket(uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_admission_ticket(uuid,uuid,text,text) TO photo_scheduler;
