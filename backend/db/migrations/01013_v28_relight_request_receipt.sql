-- +goose Up
-- Historical acknowledgement bound to the exact preliminary authored request.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_relight_request_receipt(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_request jsonb,p_pin jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb;receipt jsonb;
BEGIN
 receipt:=photo.read_v28_relight_queued_receipt(p_tenant,p_run,p_actor,p_session);
 IF receipt IS NULL THEN RETURN NULL;END IF;
 SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF config IS DISTINCT FROM jsonb_build_object('project_id',p_project,'v28_relight_request',p_request,'v28_relight_pin',p_pin,'auth_session_id',p_session)
 OR p_request->>'run_id' IS DISTINCT FROM p_run::text
 OR p_request->>'workflow_id' IS DISTINCT FROM receipt->>'workflow_id' THEN RETURN NULL;END IF;
 RETURN receipt;
END $$;
ALTER FUNCTION photo.read_v28_relight_request_receipt(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_relight_request_receipt(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_relight_request_receipt(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
