-- The API role has no UPDATE privilege on capture_devices; keep revocation
-- fencing in a narrow SECURITY DEFINER function instead of broadening grants.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.capture_device_admit_tx(p_tenant uuid,p_project uuid,
 p_device uuid,p_epoch bigint) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_state text; current_epoch bigint; tenant_state text; archived timestamptz;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_device IS NULL OR
  p_epoch IS NULL OR p_epoch<1 THEN RETURN false; END IF;
 SELECT status INTO tenant_state FROM photo.tenants WHERE id=p_tenant FOR SHARE;
 IF NOT FOUND OR tenant_state<>'active' THEN RETURN false; END IF;
 SELECT archived_at INTO archived FROM photo.projects
  WHERE tenant_id=p_tenant AND id=p_project FOR SHARE;
 IF NOT FOUND OR archived IS NOT NULL THEN RETURN false; END IF;
 SELECT state,credential_epoch INTO current_state,current_epoch
  FROM photo.capture_devices
  WHERE tenant_id=p_tenant AND project_id=p_project AND id=p_device FOR SHARE;
 RETURN FOUND AND current_state='active' AND current_epoch=p_epoch;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_device_admit_tx(uuid,uuid,uuid,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_device_admit_tx(uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_device_admit_tx(uuid,uuid,uuid,bigint) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
