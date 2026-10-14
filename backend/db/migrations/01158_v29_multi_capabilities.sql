-- +goose Up
-- Read-only producer availability. A successful check is not job admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_capabilities_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb,p_operation text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_operation IS NULL OR p_operation NOT IN('optical.panorama','optical.focus_stack') THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM operation FROM photo.operation_registry WHERE operation=p_operation AND version='1' AND resource_class='cpu' AND v2_enabled AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.lock_v29_multi_qualification(p_tenant,p_project,p_actor,p_session,p_pin,p_operation) IS TRUE;
END $$;
ALTER FUNCTION photo.v29_multi_capabilities_ready(uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_capabilities_ready(uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_multi_capabilities_ready(uuid,uuid,uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
