-- A sealed analysis can be replayed from its original idempotency ticket
-- without reopening an expired selection snapshot or current draft recipes.
-- Pending drafts still pass through the full freeze/admission checks.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_replay(p_tenant uuid,p_actor uuid,p_auth_session uuid,
 p_run uuid,p_key text,p_request_sha text)
RETURNS TABLE(outcome text,run_id uuid,workflow_id uuid)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE idem photo.idempotency_records%ROWTYPE; r photo.runs%ROWTYPE;
 workflow photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_auth_session IS NULL OR p_run IS NULL OR
  p_key IS NULL OR p_key COLLATE "C" !~ '^[!-~]{1,128}$' OR
  p_request_sha IS NULL OR p_request_sha !~ '^[0-9a-f]{64}$' THEN
  RETURN QUERY SELECT 'invalid'::text,NULL::uuid,NULL::uuid; RETURN;
 END IF;
 SELECT * INTO idem FROM photo.idempotency_records i
  WHERE i.tenant_id=p_tenant AND i.principal_type='user'
   AND i.principal_id=p_actor AND i.route='V2.Admit' AND i.key=p_key;
 IF idem.id IS NULL THEN
  RETURN QUERY SELECT 'missing'::text,NULL::uuid,NULL::uuid; RETURN;
 END IF;
 IF idem.request_hash<>p_request_sha THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::uuid,NULL::uuid; RETURN;
 END IF;
 IF idem.resource_id IS NULL THEN
  RETURN QUERY SELECT 'missing'::text,NULL::uuid,NULL::uuid; RETURN;
 END IF;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=idem.resource_id AND x.id=p_run AND x.created_by=p_actor
  AND x.kind='v2';
 SELECT * INTO workflow FROM photo.v2_workflows w
  WHERE w.tenant_id=p_tenant AND w.run_id=r.id
   AND w.workflow_type='session' AND w.renderer_version='v2.2';
 IF r.id IS NULL OR workflow.id IS NULL THEN
  RETURN QUERY SELECT 'expired'::text,NULL::uuid,NULL::uuid; RETURN;
 END IF;
 IF NOT photo.v2_editor_authorized(p_tenant,workflow.project_id,
  p_actor,p_auth_session) OR NOT EXISTS(
   SELECT 1 FROM photo.shoot_sessions s WHERE s.tenant_id=p_tenant
    AND s.id::text=r.config_snapshot->>'session_id'
    AND s.project_id=workflow.project_id) THEN
  RAISE EXCEPTION 'V22_ANALYSIS_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 IF idem.response_status=202 AND r.graph_sealed AND
  idem.response_body->>'run_id'=r.id::text AND
  idem.response_body->>'workflow_id'=workflow.id::text AND
  idem.response_body->>'state'='queued' THEN
  RETURN QUERY SELECT 'replayed'::text,r.id,workflow.id; RETURN;
 END IF;
 IF idem.response_status IS NULL AND r.status='draft' AND
  NOT r.graph_sealed AND NOT r.cancel_requested THEN
  RETURN QUERY SELECT 'pending'::text,r.id,workflow.id; RETURN;
 END IF;
 RETURN QUERY SELECT 'expired'::text,NULL::uuid,NULL::uuid;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_replay(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_replay(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_replay(uuid,uuid,uuid,uuid,text,text)
 TO photo_scheduler;
