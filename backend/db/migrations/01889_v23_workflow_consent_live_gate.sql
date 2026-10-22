-- +goose Up
-- Every consent pinned to the learning workflow must still denote the same
-- live grant. In particular, a changed purpose must not remove a matching
-- grant from the identity-specific revocation check.
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
    grant_row.purpose<>(CASE WHEN profile.person_id IS NULL THEN
     'learning_photographer' ELSE 'learning_person' END) OR
    grant_row.person_id IS DISTINCT FROM profile.person_id))
 AND NOT EXISTS(SELECT 1 FROM photo.learning_versions version
  JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
   AND profile.id=version.profile_id
  LEFT JOIN photo.project_persons person ON person.tenant_id=profile.tenant_id
   AND person.project_id=profile.project_id AND person.id=profile.person_id
  WHERE version.tenant_id=p_tenant AND version.id=p_version AND
   profile.person_id IS NOT NULL AND
   (person.id IS NULL OR person.state<>'active'))
 AND NOT EXISTS(SELECT 1 FROM photo.learning_versions version
  JOIN photo.learning_profiles profile ON profile.tenant_id=version.tenant_id
   AND profile.id=version.profile_id
  JOIN photo.workflow_consent_links link ON link.tenant_id=version.tenant_id
   AND link.project_id=version.project_id AND
   link.workflow_id=version.workflow_id
  LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
   AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
  LEFT JOIN photo.project_persons person ON person.tenant_id=grant_row.tenant_id
   AND person.project_id=grant_row.project_id AND person.id=grant_row.person_id
  WHERE version.tenant_id=p_tenant AND version.id=p_version AND
   (grant_row.id IS NULL OR grant_row.status<>'granted' OR
    grant_row.epoch<>link.consent_epoch OR
    (grant_row.purpose NOT IN ('identity_matching',
      CASE WHEN profile.person_id IS NULL THEN 'learning_photographer'
       ELSE 'learning_person' END)) OR
    (grant_row.purpose<>'identity_matching' AND
     grant_row.person_id IS DISTINCT FROM profile.person_id) OR
    (grant_row.purpose='identity_matching' AND
     (person.id IS NULL OR person.state<>'active'))));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_learning_version_consents_live(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_learning_version_consents_live(uuid,uuid)
 FROM PUBLIC;
