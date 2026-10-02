-- +goose Up
-- Authority gate inside the draft creation transaction, not job admission alone.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_admission_scope(p_tenant uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_params jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation='region.preview' AND version='1' AND v2_enabled)
  THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 IF NOT photo.v26_region_snapshot_scope(p_tenant,p_snapshot,p_actor,p_params) THEN RETURN false; END IF;
 RETURN photo.v26_region_locked_scope(p_tenant,p_actor,p_session,p_params);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_admission_scope(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_admission_scope(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_admission_scope(uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
