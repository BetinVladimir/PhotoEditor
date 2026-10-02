-- Safety review and explicit activation are separate CAS transitions.
-- Both serialize with consent revoke on the tenant lifecycle row.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_learning_version_consents_live(p_tenant uuid,
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
   person.state<>'active');
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_learning_version_consents_live(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_learning_version_consents_live(uuid,uuid)
 FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION photo.review_v23_learning_safety(p_tenant uuid,p_version uuid,
 p_expected bigint,p_evaluation_sha text,p_accept boolean,p_reason text,
 p_actor uuid,p_session uuid) RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE version photo.learning_versions%ROWTYPE;
 profile photo.learning_profiles%ROWTYPE;
 eligibility photo.version_eligibility%ROWTYPE;
 evaluation photo.artifacts%ROWTYPE; review_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_version IS NULL OR p_actor IS NULL OR
  p_session IS NULL OR p_expected IS NULL OR p_expected<1 OR
  p_accept IS NULL OR coalesce(p_evaluation_sha,'') !~ '^[0-9a-f]{64}$' OR
  length(coalesce(p_reason,'')) NOT BETWEEN 1 AND 256 THEN
  RAISE EXCEPTION 'V23_SAFETY_REVIEW_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO version FROM photo.learning_versions v WHERE v.tenant_id=p_tenant
  AND v.id=p_version;
 IF version.id IS NULL THEN RETURN NULL; END IF;
 SELECT * INTO profile FROM photo.learning_profiles p WHERE p.tenant_id=p_tenant
  AND p.id=version.profile_id FOR UPDATE;
 IF profile.id IS NULL OR profile.project_id<>version.project_id OR
  NOT EXISTS(SELECT 1 FROM photo.projects project WHERE project.tenant_id=p_tenant
   AND project.id=profile.project_id AND project.archived_at IS NULL) OR
  NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) auth
   WHERE auth.role='owner' OR (profile.created_by=p_actor AND
    auth.role IN ('admin','editor') AND
    photo.v2_editor_authorized(p_tenant,profile.project_id,p_actor,p_session)))
  THEN RETURN NULL; END IF;
 SELECT * INTO eligibility FROM photo.version_eligibility state
  WHERE state.tenant_id=p_tenant AND state.project_id=version.project_id
   AND state.version_id=p_version FOR UPDATE;
 SELECT * INTO evaluation FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=version.evaluation_artifact_id FOR SHARE;
 IF eligibility.id IS NULL OR eligibility.revision<>p_expected OR
  eligibility.status<>'needs_review' OR eligibility.revision>=9007199254740991 OR
  evaluation.id IS NULL OR evaluation.sha256<>p_evaluation_sha OR
  evaluation.state<>'ready' OR evaluation.output_role<>'document' OR
  evaluation.media_type<>'application/json' OR evaluation.verified_at IS NULL OR
  evaluation.object_version IS NULL OR
  (evaluation.expires_at IS NOT NULL AND
   evaluation.expires_at<=clock_timestamp()) OR
  NOT photo.v23_learning_version_consents_live(p_tenant,p_version)
  THEN RETURN NULL; END IF;
 INSERT INTO photo.learning_safety_reviews(tenant_id,project_id,version_id,
  evaluation_sha,actor_id,accept,reason)
 VALUES(p_tenant,version.project_id,p_version,p_evaluation_sha,p_actor,
  p_accept,p_reason) RETURNING id INTO review_id;
 UPDATE photo.version_eligibility SET status=CASE WHEN p_accept THEN
  'eligible' ELSE 'rejected' END,reason=CASE WHEN p_accept THEN NULL
  ELSE 'HUMAN_SAFETY_REJECTED' END,revision=revision+1,
  updated_at=clock_timestamp() WHERE id=eligibility.id;
 RETURN review_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.review_v23_learning_safety(uuid,uuid,bigint,text,boolean,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.review_v23_learning_safety(uuid,uuid,bigint,text,boolean,text,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.review_v23_learning_safety(uuid,uuid,bigint,text,boolean,text,uuid,uuid)
 TO photo_api;

-- +goose StatementBegin
CREATE FUNCTION photo.activate_v23_learning_profile(p_tenant uuid,p_profile uuid,
 p_expected bigint,p_version uuid,p_reason text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE profile photo.learning_profiles%ROWTYPE;
 version photo.learning_versions%ROWTYPE;
 eligibility photo.version_eligibility%ROWTYPE;
 current_version photo.learning_versions%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_profile IS NULL OR p_version IS NULL OR
  p_actor IS NULL OR p_session IS NULL OR p_expected IS NULL OR
  p_expected<1 OR p_reason NOT IN ('promote','rollback') THEN
  RAISE EXCEPTION 'V23_ACTIVATION_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO profile FROM photo.learning_profiles p WHERE p.tenant_id=p_tenant
  AND p.id=p_profile FOR UPDATE;
 IF profile.id IS NULL OR profile.revision<>p_expected OR
  profile.revision>=9007199254740991 OR
  profile.active_version_id IS NOT DISTINCT FROM p_version OR
  NOT EXISTS(SELECT 1 FROM photo.projects project WHERE project.tenant_id=p_tenant
   AND project.id=profile.project_id AND project.archived_at IS NULL) OR
  NOT EXISTS(SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) auth
   WHERE auth.role='owner' OR (profile.created_by=p_actor AND
    auth.role IN ('admin','editor') AND
    photo.v2_editor_authorized(p_tenant,profile.project_id,p_actor,p_session)))
  THEN RETURN false; END IF;
 SELECT * INTO version FROM photo.learning_versions v WHERE v.tenant_id=p_tenant
  AND v.project_id=profile.project_id AND v.profile_id=profile.id
  AND v.id=p_version;
 SELECT * INTO eligibility FROM photo.version_eligibility state
  WHERE state.tenant_id=p_tenant AND state.project_id=profile.project_id
   AND state.version_id=p_version FOR UPDATE;
 IF version.id IS NULL OR eligibility.id IS NULL OR
  eligibility.status<>'eligible' OR
  NOT photo.v23_learning_version_consents_live(p_tenant,p_version) OR
  NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=version.model_artifact_id AND a.state='ready' AND
    a.output_role='document' AND a.media_type='application/json' AND
    a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND
    (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) OR
  NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.id=version.evaluation_artifact_id AND a.state='ready' AND
    a.output_role='document' AND a.media_type='application/json' AND
    a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND
    (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))
  THEN RETURN false; END IF;
 IF profile.active_version_id IS NULL THEN
  IF p_reason<>'promote' THEN RETURN false; END IF;
 ELSE
  SELECT * INTO current_version FROM photo.learning_versions v
   WHERE v.tenant_id=p_tenant AND v.id=profile.active_version_id;
  IF current_version.id IS NULL OR
   (p_reason='promote' AND version.version_no<=current_version.version_no) OR
   (p_reason='rollback' AND version.version_no>=current_version.version_no)
   THEN RETURN false; END IF;
 END IF;
 INSERT INTO photo.profile_activations(tenant_id,project_id,profile_id,
  version_id,actor_id,reason,previous_version_id)
 VALUES(p_tenant,profile.project_id,profile.id,version.id,p_actor,p_reason,
  profile.active_version_id);
 UPDATE photo.learning_profiles SET active_version_id=version.id,
  revision=revision+1,updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=profile.id AND revision=p_expected;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'V23_ACTIVATION_RACE' USING ERRCODE='40001';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.activate_v23_learning_profile(uuid,uuid,bigint,uuid,text,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v23_learning_profile(uuid,uuid,bigint,uuid,text,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.activate_v23_learning_profile(uuid,uuid,bigint,uuid,text,uuid,uuid)
 TO photo_api;
