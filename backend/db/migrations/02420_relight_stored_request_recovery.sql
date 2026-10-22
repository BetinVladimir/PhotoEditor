-- +goose Up
-- Read historical acknowledgement with stored server-owned qualification pins.
-- No publication/admission authority is supplied by the caller or this reader.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_relight_stored_request_outcome(p_tenant uuid,p_project uuid,p_run uuid,p_actor uuid,p_session uuid,p_request jsonb)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE config jsonb;pin jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN jsonb_build_object('outcome','denied');END IF;
 SELECT config_snapshot INTO config FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR SHARE;
 IF NOT photo.v28_receipt_actor_live(p_tenant,p_project,p_actor,p_session) THEN RETURN jsonb_build_object('outcome','denied');END IF;
 IF config IS NULL THEN
  RETURN photo.read_v28_relight_request_outcome(p_tenant,p_project,p_run,p_actor,p_session,p_request,'{}'::jsonb);
 END IF;
 pin:=config->'v28_relight_pin';
 IF jsonb_typeof(pin) IS DISTINCT FROM 'object' THEN RETURN jsonb_build_object('outcome','denied');END IF;
 -- Existing strict reader binds project, actor, session and full authored
 -- request, then repeats source/qualification/closure checks for the receipt.
 RETURN photo.read_v28_relight_request_outcome(p_tenant,p_project,p_run,p_actor,p_session,p_request,pin);
END $$;
ALTER FUNCTION photo.read_v28_relight_stored_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_relight_stored_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_relight_stored_request_outcome(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
