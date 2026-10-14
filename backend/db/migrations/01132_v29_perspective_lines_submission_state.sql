-- +goose Up
-- Current principal/source authorization precedes saved submission disclosure.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_perspective_lines_submission_state(p_tenant uuid,p_project uuid,p_run uuid,
 p_actor uuid,p_session uuid,p_snapshot uuid,p_base jsonb,p_policy text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;plan jsonb;stage text;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL OR p_run='00000000-0000-0000-0000-000000000000'::uuid
 OR photo.v28_local_portrait_source_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base) IS DISTINCT FROM true
 THEN RETURN NULL;END IF;
 SELECT * INTO r FROM photo.runs WHERE id=p_run;
 IF NOT FOUND THEN RETURN jsonb_build_object('stage','missing','task_id',NULL);END IF;
 plan:=r.config_snapshot->'v29_perspective_line_detection';
 IF r.tenant_id IS DISTINCT FROM p_tenant OR NOT r.v29_perspective_line_detection OR r.created_by IS DISTINCT FROM p_actor
 OR plan->>'project_id' IS DISTINCT FROM p_project::text OR plan->>'snapshot_id' IS DISTINCT FROM p_snapshot::text
 OR plan->'base' IS DISTINCT FROM p_base THEN
  RETURN jsonb_build_object('stage','conflict','task_id',NULL);END IF;
 IF r.v29_perspective_lines_execution_policy IS NOT NULL AND r.v29_perspective_lines_execution_policy IS DISTINCT FROM p_policy THEN
  RETURN jsonb_build_object('stage','conflict','task_id',NULL);END IF;
 IF r.status='draft' AND NOT r.cancel_requested AND NOT r.graph_sealed THEN
  -- Saved draft authority stays immutable. A new auth session may read an
  -- already queued outcome, but cannot resume an old-session draft implicitly.
  IF plan->>'auth_session_id' IS DISTINCT FROM p_session::text THEN
   RETURN jsonb_build_object('stage','conflict','task_id',NULL);END IF;
  stage:=CASE WHEN r.v29_perspective_lines_seal_receipt IS NULL THEN 'draft' ELSE 'sealed' END;
 ELSIF r.graph_sealed AND NOT r.cancel_requested AND r.status IN('queued','running','waiting_review','completed') THEN stage:=r.status;
 ELSE stage:='expired';END IF;
 RETURN jsonb_build_object('stage',stage,'task_id',(plan->>'task_id')::uuid);
END $$;
ALTER FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.v29_perspective_lines_submission_state(uuid,uuid,uuid,uuid,uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
