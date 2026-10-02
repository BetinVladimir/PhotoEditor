-- A learning version that used identity matching must remain disabled after
-- the matching grant is revoked, even if its separate learning grant lives.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v23_learning_version_consents_live(p_tenant uuid,
 p_version uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.learning_example_links link
  WHERE link.tenant_id=p_tenant AND link.version_id=p_version)
 AND NOT EXISTS(SELECT 1 FROM photo.learning_example_links link
  JOIN photo.learning_versions version ON version.tenant_id=link.tenant_id
   AND version.id=link.version_id
  JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
   AND profile.id=version.profile_id
  LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
   AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
  WHERE link.tenant_id=p_tenant AND link.version_id=p_version
   AND (grant_row.id IS NULL OR grant_row.status<>'granted' OR
    grant_row.epoch<>link.consent_epoch OR
    grant_row.purpose<>CASE WHEN profile.person_id IS NULL THEN
     'learning_photographer' ELSE 'learning_person' END OR
    grant_row.person_id IS DISTINCT FROM profile.person_id))
 AND NOT EXISTS(SELECT 1 FROM photo.learning_versions version
  JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
   AND profile.id=version.profile_id
  JOIN photo.project_persons person ON person.tenant_id=profile.tenant_id
   AND person.project_id=profile.project_id AND person.id=profile.person_id
  WHERE version.tenant_id=p_tenant AND version.id=p_version AND
   person.state<>'active')
 AND NOT EXISTS(SELECT 1 FROM photo.learning_versions version
  JOIN photo.workflow_consent_links link ON link.tenant_id=version.tenant_id
   AND link.project_id=version.project_id AND
   link.workflow_id=version.workflow_id
  LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
   AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
  WHERE version.tenant_id=p_tenant AND version.id=p_version AND
   grant_row.purpose='identity_matching' AND
   (grant_row.status<>'granted' OR
    grant_row.epoch<>link.consent_epoch OR
    NOT EXISTS(SELECT 1 FROM photo.project_persons person
     WHERE person.tenant_id=grant_row.tenant_id AND
      person.project_id=grant_row.project_id AND
      person.id=grant_row.person_id AND person.state='active')));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_learning_version_consents_live(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_learning_version_consents_live(uuid,uuid)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.disable_v23_learning_on_identity_revoke() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF OLD.purpose='identity_matching' AND OLD.status='granted' AND
  (NEW.status<>'granted' OR NEW.epoch<>OLD.epoch) THEN
  IF EXISTS(SELECT 1 FROM photo.version_eligibility state
   JOIN photo.learning_versions version ON version.tenant_id=state.tenant_id
    AND version.id=state.version_id
   JOIN photo.workflow_consent_links link ON link.tenant_id=version.tenant_id
    AND link.project_id=version.project_id AND
    link.workflow_id=version.workflow_id
   WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
    AND link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch
    AND state.status<>'disabled' AND state.revision>=9007199254740991) OR
   EXISTS(SELECT 1 FROM photo.learning_profiles profile
    JOIN photo.learning_versions version ON version.tenant_id=profile.tenant_id
     AND version.id=profile.active_version_id
    JOIN photo.workflow_consent_links link ON link.tenant_id=version.tenant_id
     AND link.project_id=version.project_id AND
     link.workflow_id=version.workflow_id
    WHERE link.tenant_id=OLD.tenant_id AND link.project_id=OLD.project_id
     AND link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch
     AND profile.revision>=9007199254740991) THEN
   RAISE EXCEPTION 'V23_IDENTITY_REVISION_EXHAUSTED' USING ERRCODE='22003';
  END IF;
  UPDATE photo.learning_profiles profile SET active_version_id=NULL,
   revision=profile.revision+1,updated_at=clock_timestamp()
   FROM photo.learning_versions version
   JOIN photo.workflow_consent_links link ON link.tenant_id=version.tenant_id
    AND link.project_id=version.project_id AND
    link.workflow_id=version.workflow_id
   WHERE profile.tenant_id=OLD.tenant_id AND
    profile.project_id=OLD.project_id AND
    profile.active_version_id=version.id AND
    link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch;
  UPDATE photo.version_eligibility state SET status='disabled',
   reason='IDENTITY_CONSENT_REVOKED',revision=state.revision+1,
   updated_at=clock_timestamp()
   FROM photo.learning_versions version
   JOIN photo.workflow_consent_links link ON link.tenant_id=version.tenant_id
    AND link.project_id=version.project_id AND
    link.workflow_id=version.workflow_id
   WHERE state.tenant_id=OLD.tenant_id AND state.project_id=OLD.project_id
    AND state.version_id=version.id AND state.status<>'disabled' AND
    link.consent_id=OLD.id AND link.consent_epoch=OLD.epoch;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.disable_v23_learning_on_identity_revoke()
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.disable_v23_learning_on_identity_revoke()
 FROM PUBLIC;
CREATE TRIGGER v23_learning_identity_consent_revoke
 AFTER UPDATE OF status,epoch ON photo.consent_grants
 FOR EACH ROW EXECUTE FUNCTION photo.disable_v23_learning_on_identity_revoke();
