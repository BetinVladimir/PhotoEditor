-- +goose Up
-- Strict read-only receipt classification. Legacy validators retain every
-- source/qualification/closure check; their NULL is never proof of absence.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_receipt_actor_live(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND EXISTS(SELECT FROM photo.sessions s WHERE s.id=p_session AND s.user_id=p_actor
  AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp())
$$;
ALTER FUNCTION photo.v28_receipt_actor_live(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_receipt_actor_live(uuid,uuid,uuid,uuid) FROM PUBLIC;

CREATE FUNCTION photo.read_v28_relight_request_outcome(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_request jsonb,p_pin jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;body jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN jsonb_build_object('outcome','denied');END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF r.id IS NULL THEN RETURN jsonb_build_object('outcome','absent');END IF;
 IF r.created_by IS DISTINCT FROM p_actor OR r.kind IS DISTINCT FROM 'v2' THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF r.config_snapshot IS DISTINCT FROM jsonb_build_object('project_id',p_project,'v28_relight_request',p_request,'v28_relight_pin',p_pin,'auth_session_id',p_session) THEN RETURN jsonb_build_object('outcome','conflict');END IF;
 IF r.v28_relight_queued_receipt IS NULL AND NOT r.graph_sealed AND NOT r.cancel_requested THEN RETURN jsonb_build_object('outcome','absent');END IF;
 body:=photo.read_v28_relight_request_receipt(p_tenant,p_project,p_run,p_actor,p_session,p_request,p_pin);
 IF body IS NULL OR NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 RETURN jsonb_build_object('outcome','receipt','receipt',body);
END $$;
ALTER FUNCTION photo.read_v28_relight_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_relight_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_relight_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;

CREATE FUNCTION photo.read_v28_relight_analysis_outcome(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_expected jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;body jsonb;project uuid;
BEGIN
 project:=(p_expected->'config'->>'project_id')::uuid;
 IF project IS NULL OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v28_receipt_actor_live(p_tenant,project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 -- Same tenant lock mode as the existing analysis receipt validator.
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN jsonb_build_object('outcome','denied');END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT photo.v28_receipt_actor_live(p_tenant,project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF r.id IS NULL THEN RETURN jsonb_build_object('outcome','absent');END IF;
 IF r.created_by IS DISTINCT FROM p_actor OR r.kind IS DISTINCT FROM 'v2' THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF r.config_snapshot->'v28_analysis_plan' IS DISTINCT FROM p_expected THEN RETURN jsonb_build_object('outcome','conflict');END IF;
 IF r.v28_analysis_queued_receipt IS NULL AND NOT r.graph_sealed AND NOT r.cancel_requested THEN RETURN jsonb_build_object('outcome','absent');END IF;
 body:=photo.read_v28_relight_analysis_queued_receipt(p_tenant,p_run,p_actor,p_session,p_expected);
 IF body IS NULL OR NOT photo.v28_receipt_actor_live(p_tenant,project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 RETURN jsonb_build_object('outcome','receipt','receipt',body);
EXCEPTION WHEN invalid_text_representation THEN RETURN jsonb_build_object('outcome','denied');
END $$;
ALTER FUNCTION photo.read_v28_relight_analysis_outcome(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_relight_analysis_outcome(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_relight_analysis_outcome(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
