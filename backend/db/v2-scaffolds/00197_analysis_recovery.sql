-- Persist exact compiler inputs with the pending idempotency record in the
-- same transaction that creates the draft. Seal replaces this body with 202.
-- +goose StatementBegin
CREATE FUNCTION photo.save_v22_analysis_recovery(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text,p_body jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; r photo.runs%ROWTYPE;
 workflow photo.v2_workflows%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 IF p_body IS NULL OR jsonb_typeof(p_body)<>'object' OR
  (p_body-'schema_version'-'snapshot'-'context'-'context_sha'-'policy_sha'-
   'model_lock_sha'-'reserved_bytes')<>'{}'::jsonb OR
  p_body->>'schema_version'<>'1' OR
  octet_length(p_body::text)>10485760 OR
  coalesce(p_body->>'context_sha','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_body->>'policy_sha','') !~ '^[0-9a-f]{64}$' OR
  coalesce(p_body->>'model_lock_sha','') !~ '^[0-9a-f]{64}$' OR
  jsonb_typeof(p_body->'snapshot')<>'object' OR
  jsonb_typeof(p_body->'context')<>'object' THEN
  RAISE EXCEPTION 'V22_RECOVERY_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO idem FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant
  AND i.principal_type='user' AND i.principal_id=p_actor AND i.route='V2.Admit'
  AND i.key=p_key AND i.request_hash=p_request_sha FOR UPDATE;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run
  AND x.id=idem.resource_id AND x.created_by=p_actor AND x.kind='v2';
 SELECT * INTO workflow FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.run_id=p_run AND w.workflow_type='session';
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved';
 IF idem.id IS NULL OR r.id IS NULL OR workflow.id IS NULL OR q.id IS NULL OR
  idem.response_status IS NOT NULL OR r.status<>'draft' OR r.graph_sealed OR
  NOT photo.v2_editor_authorized(p_tenant,workflow.project_id,p_actor,p_auth_session) OR
  r.config_snapshot->'v22_session_plan'->>'snapshot_digest' IS DISTINCT FROM
   p_body->'snapshot'->>'snapshot_digest' OR
  r.config_snapshot->'v22_session_plan'->>'context_sha' IS DISTINCT FROM
   p_body->>'context_sha' OR
  workflow.policy_sha IS DISTINCT FROM p_body->>'policy_sha' OR
  q.bytes::text IS DISTINCT FROM p_body->>'reserved_bytes' THEN
  RAISE EXCEPTION 'V22_RECOVERY_CONFLICT' USING ERRCODE='23514';
 END IF;
 IF idem.response_body IS NOT NULL AND idem.response_body<>p_body THEN
  RAISE EXCEPTION 'V22_RECOVERY_CONFLICT' USING ERRCODE='23514';
 END IF;
 UPDATE photo.idempotency_records SET response_body=p_body WHERE id=idem.id
  AND response_body IS NULL;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.save_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text,jsonb)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.read_v22_analysis_recovery(p_tenant uuid,p_actor uuid,
 p_auth_session uuid,p_run uuid,p_key text,p_request_sha text)
RETURNS jsonb LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; r photo.runs%ROWTYPE;
 workflow photo.v2_workflows%ROWTYPE; q photo.quota_reservations%ROWTYPE;
BEGIN
 SELECT * INTO idem FROM photo.idempotency_records i WHERE i.tenant_id=p_tenant
  AND i.principal_type='user' AND i.principal_id=p_actor AND i.route='V2.Admit'
  AND i.key=p_key AND i.request_hash=p_request_sha;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant AND x.id=p_run
  AND x.id=idem.resource_id AND x.created_by=p_actor AND x.kind='v2';
 SELECT * INTO workflow FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
  AND w.run_id=p_run AND w.workflow_type='session';
 SELECT * INTO q FROM photo.quota_reservations x WHERE x.tenant_id=p_tenant
  AND x.holder_type='run' AND x.holder_id=p_run AND x.state='reserved'
  AND x.expires_at>clock_timestamp();
 IF idem.id IS NULL OR r.id IS NULL OR workflow.id IS NULL OR q.id IS NULL OR
  idem.response_status IS NOT NULL OR r.status<>'draft' OR r.graph_sealed OR
  idem.response_body IS NULL OR
  NOT photo.v2_editor_authorized(p_tenant,workflow.project_id,p_actor,p_auth_session) OR
  r.config_snapshot->'v22_session_plan'->>'snapshot_digest' IS DISTINCT FROM
   idem.response_body->'snapshot'->>'snapshot_digest' OR
  r.config_snapshot->'v22_session_plan'->>'context_sha' IS DISTINCT FROM
   idem.response_body->>'context_sha' OR
  workflow.policy_sha IS DISTINCT FROM idem.response_body->>'policy_sha' OR
  q.bytes::text IS DISTINCT FROM idem.response_body->>'reserved_bytes' THEN
  RETURN NULL;
 END IF;
 RETURN idem.response_body;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v22_analysis_recovery(uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;
