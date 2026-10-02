-- A revoked learning grant immediately disables versions that used its old
-- epoch and removes their active profile pointers. Historical links remain.
-- The caller of change_v2_consent holds the tenant lifecycle lock, serializing
-- this trigger with version adoption, safety review and activation.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.disable_v23_learning_on_consent_revoke() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.status='granted' AND NEW.status='revoked' AND
  OLD.purpose IN('learning_photographer','learning_person') THEN
  IF EXISTS(SELECT 1 FROM photo.version_eligibility state
   JOIN photo.learning_example_links link ON link.tenant_id=state.tenant_id
    AND link.project_id=state.project_id AND link.version_id=state.version_id
   WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
    AND link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch
    AND state.status<>'disabled' AND state.revision>=9007199254740991) OR
   EXISTS(SELECT 1 FROM photo.learning_profiles profile
    JOIN photo.learning_example_links link ON link.tenant_id=profile.tenant_id
     AND link.project_id=profile.project_id AND
      link.version_id=profile.active_version_id
    WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
     AND link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch
     AND profile.revision>=9007199254740991) THEN
   RAISE EXCEPTION 'V23_LEARNING_REVISION_EXHAUSTED' USING ERRCODE='22003';
  END IF;
  UPDATE photo.learning_profiles profile SET active_version_id=NULL,
   revision=profile.revision+1,updated_at=clock_timestamp()
   WHERE profile.tenant_id=OLD.tenant_id AND
    profile.project_id=OLD.project_id AND profile.active_version_id IS NOT NULL
    AND EXISTS(SELECT 1 FROM photo.learning_example_links link
     WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
      AND link.version_id=profile.active_version_id AND
       link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch);
  UPDATE photo.version_eligibility state SET status='disabled',
   reason='CONSENT_REVOKED',revision=state.revision+1,
   updated_at=clock_timestamp()
   WHERE state.tenant_id=OLD.tenant_id AND state.project_id=OLD.project_id
    AND state.status<>'disabled'
    AND EXISTS(SELECT 1 FROM photo.learning_example_links link
     WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
      AND link.version_id=state.version_id AND
       link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch);
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.disable_v23_learning_on_consent_revoke()
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.disable_v23_learning_on_consent_revoke()
 FROM PUBLIC;
CREATE TRIGGER v23_learning_consent_revoke
 AFTER UPDATE OF status,epoch ON photo.consent_grants
 FOR EACH ROW EXECUTE FUNCTION photo.disable_v23_learning_on_consent_revoke();
