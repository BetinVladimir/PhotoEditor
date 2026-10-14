-- +goose Up
-- Take exclusive admission locks before subsidiary SHARE locks, avoiding upgrades.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_donor_graph_owner(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND created_by=p_actor FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.status<>'draft' OR r.graph_sealed OR r.cancel_requested
 OR r.v28_donor_descriptor_jcs IS NULL OR r.v28_donor_params_intent IS NULL
 OR r.v28_donor_params_intent->>'actor_id' IS DISTINCT FROM p_actor::text
 OR r.v28_donor_params_intent->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN false;END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.lock_v28_donor_graph_owner(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_donor_graph_owner(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_donor_graph_owner(uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
