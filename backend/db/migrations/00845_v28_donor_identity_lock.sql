-- +goose Up
-- Current identity/consent gate only; session/source/face document pins are separate.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v28_donor_identity(p_tenant uuid,p_project uuid,p_person uuid,
 p_consent uuid,p_epoch text,p_source_observation uuid,p_donor_observation uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE person photo.project_persons%ROWTYPE;source_observation photo.person_observations%ROWTYPE;
 donor_observation photo.person_observations%ROWTYPE;
BEGIN
 IF p_person IS NULL OR p_consent IS NULL OR p_source_observation IS NULL OR p_donor_observation IS NULL
  OR p_source_observation=p_donor_observation OR coalesce(p_epoch,'') !~ '^[1-9][0-9]{0,18}$'
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=p_project AND archived_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO person FROM photo.project_persons WHERE tenant_id=p_tenant AND project_id=p_project AND id=p_person AND state='active' FOR SHARE;
 IF NOT FOUND OR person.consent_epoch::text IS DISTINCT FROM p_epoch THEN RETURN false;END IF;
 PERFORM id FROM photo.consent_grants WHERE tenant_id=p_tenant AND project_id=p_project AND id=p_consent
  AND person_id=p_person AND purpose='identity_matching' AND status='granted' AND epoch::text=p_epoch FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 -- Deterministic observation lock order across reciprocal donor requests.
 PERFORM id FROM photo.person_observations WHERE tenant_id=p_tenant AND project_id=p_project
  AND id IN(p_source_observation,p_donor_observation) ORDER BY id FOR SHARE;
 SELECT * INTO source_observation FROM photo.person_observations WHERE tenant_id=p_tenant AND project_id=p_project
  AND id=p_source_observation AND person_id=p_person AND confirmed_by IS NOT NULL;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO donor_observation FROM photo.person_observations WHERE tenant_id=p_tenant AND project_id=p_project
  AND id=p_donor_observation AND person_id=p_person AND confirmed_by IS NOT NULL;
 IF NOT FOUND OR source_observation.asset_version_id=donor_observation.asset_version_id THEN RETURN false;END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.lock_v28_donor_identity(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v28_donor_identity(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v28_donor_identity(uuid,uuid,uuid,uuid,text,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
