-- +goose Up
-- Sealed retries use the immutable decision without exposing its table to
-- photo_api. Authorization is checked on every call, including old keys.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_analysis_apply_replay(
 p_tenant uuid,p_analysis uuid,p_actor uuid,p_auth_session uuid,
 p_key text,p_decision_sha text)
RETURNS TABLE(outcome text,child_run_id uuid,workflow_id uuid)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE analysis photo.session_analyses%ROWTYPE;
 idem photo.idempotency_records%ROWTYPE;
 decision photo.v22_analysis_decisions%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_analysis IS NULL OR p_actor IS NULL
  OR p_auth_session IS NULL OR p_key IS NULL OR length(p_key) NOT BETWEEN 1 AND 256
  OR coalesce(p_decision_sha,'') !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V22_APPLY_REPLAY_INVALID' USING ERRCODE='22023';
 END IF;
 SELECT * INTO analysis FROM photo.session_analyses a
  WHERE a.tenant_id=p_tenant AND a.id=p_analysis;
 IF analysis.id IS NULL OR NOT photo.v2_editor_authorized(
  p_tenant,analysis.project_id,p_actor,p_auth_session)
  OR NOT EXISTS(SELECT 1 FROM photo.shoot_sessions s
   WHERE s.tenant_id=p_tenant AND s.id=analysis.session_id
    AND s.project_id=analysis.project_id) THEN
  RAISE EXCEPTION 'V22_APPLY_REPLAY_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO idem FROM photo.idempotency_records i
  WHERE i.tenant_id=p_tenant AND i.principal_type='user'
   AND i.principal_id=p_actor AND i.route='SessionAnalysis.Apply'
   AND i.key=p_key;
 IF idem.id IS NULL THEN
  RETURN QUERY SELECT 'absent'::text,NULL::uuid,analysis.workflow_id;
  RETURN;
 END IF;
 SELECT * INTO decision FROM photo.v22_analysis_decisions d
  WHERE d.tenant_id=p_tenant AND d.analysis_id=p_analysis
   AND d.decision_sha=p_decision_sha;
 IF decision.id IS NULL OR idem.response_status IS DISTINCT FROM 202
  OR idem.resource_id IS DISTINCT FROM decision.child_run_id
  OR idem.response_body->>'run_id' IS DISTINCT FROM decision.child_run_id::text THEN
  RETURN QUERY SELECT 'conflict'::text,NULL::uuid,analysis.workflow_id;
  RETURN;
 END IF;
 RETURN QUERY SELECT 'replayed'::text,decision.child_run_id,
  analysis.workflow_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_analysis_apply_replay(uuid,uuid,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_analysis_apply_replay(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_analysis_apply_replay(uuid,uuid,uuid,uuid,text,text)
 TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
