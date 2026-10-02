-- Bind device uploads to one active shoot and to the credential epoch that
-- admitted them. The nullable columns leave existing user/guest uploads intact.
-- +goose Up
ALTER TABLE photo.upload_sessions
 ADD COLUMN capture_session_id uuid,
 ADD COLUMN capture_credential_epoch bigint;

ALTER TABLE photo.upload_sessions
 ADD CONSTRAINT upload_device_binding CHECK (
  (principal_type='device' AND capture_session_id IS NOT NULL
   AND capture_credential_epoch IS NOT NULL AND capture_credential_epoch>0
   AND collection_id IS NOT NULL AND share_id IS NULL)
  OR (principal_type<>'device' AND capture_session_id IS NULL
   AND capture_credential_epoch IS NULL)
 );

ALTER TABLE photo.upload_sessions
 ADD CONSTRAINT upload_capture_session_fk
 FOREIGN KEY(tenant_id,project_id,capture_session_id)
 REFERENCES photo.shoot_sessions(tenant_id,project_id,id)
 DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE FUNCTION photo.enforce_device_upload_binding() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NEW.principal_type='device' THEN
  IF NOT EXISTS(SELECT 1 FROM photo.capture_devices d
   WHERE d.tenant_id=NEW.tenant_id AND d.project_id=NEW.project_id
    AND d.id=NEW.principal_id)
   OR NOT EXISTS(SELECT 1 FROM photo.shoot_sessions s
    WHERE s.tenant_id=NEW.tenant_id AND s.project_id=NEW.project_id
     AND s.id=NEW.capture_session_id AND s.collection_id=NEW.collection_id)
  THEN RAISE EXCEPTION 'CAPTURE_UPLOAD_BINDING_INVALID' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enforce_device_upload_binding() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enforce_device_upload_binding() FROM PUBLIC;
CREATE TRIGGER enforce_device_upload_binding
 BEFORE INSERT OR UPDATE OF principal_type,principal_id,project_id,
  collection_id,capture_session_id,capture_credential_epoch
 ON photo.upload_sessions FOR EACH ROW
 EXECUTE FUNCTION photo.enforce_device_upload_binding();

CREATE INDEX upload_device_session
 ON photo.upload_sessions(tenant_id,project_id,capture_session_id,created_at,id)
 WHERE principal_type='device';

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
