-- V22 project boundaries for tenant-scoped legacy FKs. The composite V22 FKs
-- cover V22-to-V22 links; these guards close legacy asset/collection chains.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v22_project_scope() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual_project uuid; expected_snapshot uuid;
BEGIN
 IF TG_OP='UPDATE' AND (NEW.tenant_id,NEW.project_id) IS DISTINCT FROM
    (OLD.tenant_id,OLD.project_id) THEN
  RAISE EXCEPTION 'V22_PROJECT_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 CASE TG_TABLE_NAME
 WHEN 'shoot_sessions' THEN
  SELECT c.project_id INTO actual_project FROM photo.collections c
   WHERE c.tenant_id=NEW.tenant_id AND c.id=NEW.collection_id;
  IF actual_project IS DISTINCT FROM NEW.project_id THEN
   RAISE EXCEPTION 'V22_COLLECTION_SCOPE_INVALID' USING ERRCODE='23514';
  END IF;
 WHEN 'session_analyses' THEN
  IF NOT EXISTS(SELECT 1 FROM photo.shoot_sessions s WHERE s.tenant_id=NEW.tenant_id
   AND s.project_id=NEW.project_id AND s.id=NEW.session_id) THEN
   RAISE EXCEPTION 'V22_SESSION_SCOPE_INVALID' USING ERRCODE='23514';
  END IF;
  SELECT w.input_snapshot_id INTO expected_snapshot FROM photo.v2_workflows w
   WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id
    AND w.id=NEW.workflow_id AND w.workflow_type='session';
  IF expected_snapshot IS DISTINCT FROM NEW.snapshot_id THEN
   RAISE EXCEPTION 'V22_ANALYSIS_WORKFLOW_INVALID' USING ERRCODE='23514';
  END IF;
 WHEN 'scene_groups' THEN
  SELECT a.project_id INTO actual_project FROM photo.asset_versions v
   JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.reference_version_id;
  IF actual_project IS DISTINCT FROM NEW.project_id THEN
   RAISE EXCEPTION 'V22_REFERENCE_SCOPE_INVALID' USING ERRCODE='23514';
  END IF;
 WHEN 'scene_members' THEN
  SELECT a.project_id INTO actual_project FROM photo.asset_versions v
   JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.asset_version_id;
  IF actual_project IS DISTINCT FROM NEW.project_id THEN
   RAISE EXCEPTION 'V22_MEMBER_SCOPE_INVALID' USING ERRCODE='23514';
  END IF;
 WHEN 'person_observations' THEN
  SELECT a.project_id INTO actual_project FROM photo.asset_versions v
   JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.asset_version_id;
  IF actual_project IS DISTINCT FROM NEW.project_id THEN
   RAISE EXCEPTION 'V22_OBSERVATION_SCOPE_INVALID' USING ERRCODE='23514';
  END IF;
 ELSE
  RAISE EXCEPTION 'V22_GUARD_TARGET_INVALID' USING ERRCODE='23514';
 END CASE;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_project_scope() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_project_scope() FROM PUBLIC;

CREATE TRIGGER shoot_sessions_scope BEFORE INSERT OR UPDATE ON photo.shoot_sessions
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_project_scope();
CREATE TRIGGER session_analyses_scope BEFORE INSERT OR UPDATE ON photo.session_analyses
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_project_scope();
CREATE TRIGGER scene_groups_scope BEFORE INSERT OR UPDATE ON photo.scene_groups
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_project_scope();
CREATE TRIGGER scene_members_scope BEFORE INSERT OR UPDATE ON photo.scene_members
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_project_scope();
CREATE TRIGGER person_observations_scope BEFORE INSERT OR UPDATE ON photo.person_observations
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_project_scope();

-- +goose StatementBegin
CREATE FUNCTION photo.guard_v22_mutable_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,photo AS $$
BEGIN
 IF TG_TABLE_NAME='project_persons' THEN
  IF (NEW.tenant_id,NEW.project_id,NEW.id) IS DISTINCT FROM
     (OLD.tenant_id,OLD.project_id,OLD.id) THEN
   RAISE EXCEPTION 'V22_PERSON_SCOPE_IMMUTABLE' USING ERRCODE='23514';
  END IF;
 ELSIF TG_TABLE_NAME='consent_grants' THEN
  IF (NEW.tenant_id,NEW.project_id,NEW.id,NEW.purpose,NEW.person_id) IS DISTINCT FROM
     (OLD.tenant_id,OLD.project_id,OLD.id,OLD.purpose,OLD.person_id) THEN
   RAISE EXCEPTION 'V22_CONSENT_SCOPE_IMMUTABLE' USING ERRCODE='23514';
  END IF;
 ELSIF TG_TABLE_NAME='person_observations' THEN
  IF (NEW.tenant_id,NEW.project_id,NEW.id,NEW.asset_version_id,NEW.face_id) IS DISTINCT FROM
     (OLD.tenant_id,OLD.project_id,OLD.id,OLD.asset_version_id,OLD.face_id) THEN
   RAISE EXCEPTION 'V22_OBSERVATION_IDENTITY_IMMUTABLE' USING ERRCODE='23514';
  END IF;
 ELSE
  RAISE EXCEPTION 'V22_GUARD_TARGET_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_mutable_identity() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_mutable_identity() FROM PUBLIC;
CREATE TRIGGER project_persons_identity BEFORE UPDATE ON photo.project_persons
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_mutable_identity();
CREATE TRIGGER consent_grants_identity BEFORE UPDATE ON photo.consent_grants
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_mutable_identity();
CREATE TRIGGER person_observations_identity BEFORE UPDATE ON photo.person_observations
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_mutable_identity();

-- Frozen workflow links may reference only an active exact consent epoch.
-- The share lock serializes admission with a concurrent revoke.
-- +goose StatementBegin
CREATE FUNCTION photo.guard_v22_workflow_consent() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_grant photo.consent_grants%ROWTYPE;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.v2_workflows w JOIN photo.runs r
  ON r.tenant_id=w.tenant_id AND r.id=w.run_id
  WHERE w.tenant_id=NEW.tenant_id AND w.project_id=NEW.project_id
   AND w.id=NEW.workflow_id AND r.status='draft' AND NOT r.graph_sealed) THEN
  RAISE EXCEPTION 'V22_CONSENT_LINK_TOO_LATE' USING ERRCODE='23514';
 END IF;
 SELECT * INTO current_grant FROM photo.consent_grants g
  WHERE g.tenant_id=NEW.tenant_id AND g.project_id=NEW.project_id
   AND g.id=NEW.consent_id FOR SHARE;
 IF current_grant.id IS NULL OR current_grant.status<>'granted'
  OR current_grant.epoch<>NEW.consent_epoch THEN
  RAISE EXCEPTION 'V22_CONSENT_EPOCH_INVALID' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.guard_v22_workflow_consent() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.guard_v22_workflow_consent() FROM PUBLIC;
CREATE TRIGGER workflow_consent_links_epoch BEFORE INSERT ON photo.workflow_consent_links
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v22_workflow_consent();

CREATE TRIGGER session_analyses_immutable BEFORE UPDATE ON photo.session_analyses
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER scene_groups_immutable BEFORE UPDATE ON photo.scene_groups
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER scene_members_immutable BEFORE UPDATE ON photo.scene_members
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER consent_events_immutable BEFORE UPDATE ON photo.consent_events
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();
CREATE TRIGGER workflow_consent_links_immutable BEFORE UPDATE ON photo.workflow_consent_links
 FOR EACH ROW EXECUTE FUNCTION photo.guard_v2_immutable();

CREATE TRIGGER shoot_sessions_revision BEFORE UPDATE ON photo.shoot_sessions
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();
CREATE TRIGGER project_persons_revision BEFORE UPDATE ON photo.project_persons
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();
CREATE TRIGGER person_observations_revision BEFORE UPDATE ON photo.person_observations
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();
CREATE TRIGGER consent_grants_revision BEFORE UPDATE ON photo.consent_grants
 FOR EACH ROW EXECUTE FUNCTION photo.bump_v2_revision();
