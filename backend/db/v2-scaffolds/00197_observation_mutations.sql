-- The caller must validate FaceObservations JSON and derive p_face from the
-- requested observation_id before calling this trusted transaction boundary.
-- Tenant lock precedes consent and person locks, matching revoke lock order.
-- +goose StatementBegin
CREATE FUNCTION photo.confirm_v22_observation(
 p_tenant uuid,p_project uuid,p_person uuid,p_expected bigint,
 p_version uuid,p_source uuid,p_source_sha text,p_face uuid,
 p_evidence uuid,p_evidence_sha text,p_consent uuid,p_epoch bigint,
 p_actor uuid,p_session uuid) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE grant_row photo.consent_grants%ROWTYPE;
 person_row photo.project_persons%ROWTYPE;
 existing photo.person_observations%ROWTYPE;
 created photo.person_observations%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_person IS NULL OR p_version IS NULL
  OR p_source IS NULL OR p_face IS NULL OR p_evidence IS NULL OR p_consent IS NULL
  OR p_actor IS NULL OR p_session IS NULL OR p_expected IS NULL OR p_expected<1
  OR p_epoch IS NULL OR p_epoch<1 OR p_source_sha !~ '^[0-9a-f]{64}$'
  OR p_evidence_sha !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'V22_OBSERVATION_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN
  RAISE EXCEPTION 'V22_OBSERVATION_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT * INTO grant_row FROM photo.consent_grants g WHERE g.tenant_id=p_tenant
  AND g.project_id=p_project AND g.id=p_consent FOR UPDATE;
 IF grant_row.id IS NULL OR grant_row.person_id IS DISTINCT FROM p_person
  OR grant_row.purpose<>'identity_matching' OR grant_row.status<>'granted'
  OR grant_row.epoch<>p_epoch THEN
  RAISE EXCEPTION 'V22_OBSERVATION_CONSENT_CONFLICT' USING ERRCODE='40001';
 END IF;
 SELECT * INTO person_row FROM photo.project_persons p WHERE p.tenant_id=p_tenant
  AND p.project_id=p_project AND p.id=p_person FOR UPDATE;
 IF person_row.id IS NULL THEN
  RAISE EXCEPTION 'V22_OBSERVATION_PERSON_NOT_FOUND' USING ERRCODE='P0002';
 END IF;
 IF person_row.state<>'active' OR person_row.revision<>p_expected
  OR person_row.revision>=9007199254740991 THEN
  RAISE EXCEPTION 'V22_OBSERVATION_REVISION_CONFLICT' USING ERRCODE='40001';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.asset_versions v
   JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
   WHERE v.tenant_id=p_tenant AND v.id=p_version AND asset.project_id=p_project
    AND asset.deleted_at IS NULL) OR NOT EXISTS(SELECT 1 FROM photo.artifacts source
   WHERE source.tenant_id=p_tenant AND source.id=p_source
    AND source.sha256=p_source_sha AND source.output_role='working_image'
    AND source.media_type='application/json' AND source.state='ready'
    AND source.object_version IS NOT NULL AND source.verified_at IS NOT NULL
    AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())) THEN
  RAISE EXCEPTION 'V22_OBSERVATION_SOURCE_INVALID' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.artifacts evidence
   JOIN photo.tasks task ON task.tenant_id=evidence.tenant_id
    AND task.id=evidence.created_by_task_id
   JOIN photo.task_results result ON result.tenant_id=task.tenant_id
    AND result.task_id=task.id AND result.id=task.canonical_result_id
   JOIN photo.output_uploads upload ON upload.tenant_id=evidence.tenant_id
    AND upload.artifact_id=evidence.id AND upload.task_id=task.id
    AND upload.attempt=result.attempt AND upload.fence=result.fencing_token
   JOIN photo.runs run ON run.tenant_id=task.tenant_id AND run.id=task.run_id
   JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id
    AND workflow.run_id=run.id AND workflow.project_id=p_project
   WHERE evidence.tenant_id=p_tenant AND evidence.id=p_evidence
    AND evidence.sha256=p_evidence_sha AND evidence.output_role='faces'
    AND evidence.media_type='application/json' AND evidence.state='ready'
    AND evidence.object_version IS NOT NULL AND evidence.verified_at IS NOT NULL
    AND (evidence.expires_at IS NULL OR evidence.expires_at>clock_timestamp())
    AND task.operation='session.describe' AND task.state='succeeded'
    AND evidence.produced_attempt=result.attempt
    AND evidence.produced_fence=result.fencing_token
    AND upload.state='ready' AND upload.role='faces'
    AND upload.sha256=evidence.sha256
    AND task.inputs @> jsonb_build_array(jsonb_build_object(
     'artifact_id',p_source::text,'sha256',p_source_sha))) THEN
  RAISE EXCEPTION 'V22_OBSERVATION_EVIDENCE_INVALID' USING ERRCODE='23514';
 END IF;
 SELECT * INTO existing FROM photo.person_observations o WHERE o.tenant_id=p_tenant
  AND o.asset_version_id=p_version AND o.face_id=p_face FOR UPDATE;
 IF existing.id IS NOT NULL THEN
  IF existing.project_id<>p_project THEN
   RAISE EXCEPTION 'V22_OBSERVATION_ASSOCIATION_CONFLICT' USING ERRCODE='23505';
  END IF;
  IF existing.person_id<>p_person THEN
   IF NOT EXISTS(SELECT 1 FROM photo.project_persons old_person
    WHERE old_person.tenant_id=p_tenant AND old_person.project_id=p_project
     AND old_person.id=existing.person_id AND old_person.state='revoked') THEN
    RAISE EXCEPTION 'V22_OBSERVATION_ASSOCIATION_CONFLICT' USING ERRCODE='23505';
   END IF;
   INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,
    resource_id,request_id,details)
   VALUES(p_tenant,'user',p_actor,'v2.observation.rebind','person_observation',
    existing.id,gen_random_uuid(),jsonb_build_object('project_id',p_project,
     'previous_person_id',existing.person_id,'person_id',p_person,
     'asset_version_id',p_version,'face_id',p_face,
     'previous_evidence_artifact_id',existing.evidence_artifact_id,
     'evidence_artifact_id',p_evidence,'consent_id',p_consent,'consent_epoch',p_epoch));
   UPDATE photo.person_observations SET person_id=p_person,
    evidence_artifact_id=p_evidence,confirmed_by=p_actor
    WHERE tenant_id=p_tenant AND id=existing.id RETURNING * INTO existing;
   UPDATE photo.project_persons SET updated_at=clock_timestamp()
    WHERE tenant_id=p_tenant AND id=p_person;
  END IF;
  RETURN jsonb_build_object('id',existing.id,'revision',existing.revision::text,
   'person_id',existing.person_id);
 END IF;
 INSERT INTO photo.person_observations(tenant_id,project_id,person_id,
  asset_version_id,face_id,evidence_artifact_id,confirmed_by)
 VALUES(p_tenant,p_project,p_person,p_version,p_face,p_evidence,p_actor)
 RETURNING * INTO created;
 UPDATE photo.project_persons SET updated_at=clock_timestamp()
  WHERE tenant_id=p_tenant AND id=p_person;
 INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,
  resource_id,request_id,details)
 VALUES(p_tenant,'user',p_actor,'v2.observation.confirm','person_observation',
  created.id,gen_random_uuid(),jsonb_build_object('project_id',p_project,
   'person_id',p_person,'asset_version_id',p_version,'face_id',p_face,
   'evidence_artifact_id',p_evidence,'consent_id',p_consent,'consent_epoch',p_epoch));
 RETURN jsonb_build_object('id',created.id,'revision',created.revision::text,
  'person_id',created.person_id);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.confirm_v22_observation(uuid,uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,text,uuid,bigint,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.confirm_v22_observation(uuid,uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,text,uuid,bigint,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.confirm_v22_observation(uuid,uuid,uuid,bigint,uuid,uuid,text,uuid,uuid,text,uuid,bigint,uuid,uuid) TO photo_api;
