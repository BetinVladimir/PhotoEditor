-- +goose Up
-- Recovery seed only. Original evidence must be loaded and verified again.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v28_donor_params_intent(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN NULL;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT v28_donor_params_intent INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run
 AND created_by=p_actor AND kind='v2' AND status='draft' AND NOT graph_sealed AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND OR request IS NULL OR request->>'actor_id' IS DISTINCT FROM p_actor::text
 OR request->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR photo.v28_donor_params_request_scope(p_tenant,p_run,request) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN request;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.read_v28_donor_params_intent(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v28_donor_params_intent(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v28_donor_params_intent(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
