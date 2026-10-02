-- V23 trusted source inventory for TrainProfile. This read is used inside
-- the future freeze transaction; it does not by itself create a snapshot.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_training_candidate_rows(
 p_tenant uuid,p_profile uuid,p_expected bigint,p_sessions uuid[],
 p_actor uuid,p_auth_session uuid)
RETURNS TABLE(event_id uuid,session_id uuid,asset_version_id uuid,
 source_content_sha text,source_lineage_sha text,
 base_document_id uuid,base_artifact_id uuid,base_sha text,
 approved_document_id uuid,approved_artifact_id uuid,approved_sha text,
 approval_id uuid,approved_at timestamptz,consent_id uuid,
 consent_epoch bigint,changed_paths jsonb,consent_live boolean,source_ready boolean,
 base_ready boolean,approved_ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE profile_row photo.learning_profiles%ROWTYPE;
 selected_count bigint; candidate_count bigint;
BEGIN
 IF p_tenant IS NULL OR p_profile IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_expected IS NULL OR p_expected<1 OR
  p_sessions IS NULL OR cardinality(p_sessions) NOT BETWEEN 6 AND 5000 OR
  EXISTS(SELECT 1 FROM unnest(p_sessions) id WHERE id IS NULL) OR
  (SELECT count(DISTINCT id) FROM unnest(p_sessions) id)<>
   cardinality(p_sessions) THEN
  RAISE EXCEPTION 'V23_TRAIN_SELECTION_INVALID' USING ERRCODE='22023';
 END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V23_TRAIN_TENANT_UNAVAILABLE' USING ERRCODE='42501'; END IF;
 SELECT * INTO profile_row FROM photo.learning_profiles p
  WHERE p.tenant_id=p_tenant AND p.id=p_profile FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'V23_TRAIN_PROFILE_MISSING' USING ERRCODE='P0002'; END IF;
 IF profile_row.revision<>p_expected THEN
  RAISE EXCEPTION 'V23_TRAIN_PROFILE_REVISION' USING ERRCODE='40001';
 END IF;
 IF NOT photo.v2_editor_authorized(p_tenant,profile_row.project_id,
  p_actor,p_auth_session) THEN
  RAISE EXCEPTION 'V23_TRAIN_FORBIDDEN' USING ERRCODE='42501';
 END IF;
 SELECT count(*) INTO selected_count FROM photo.shoot_sessions s
  WHERE s.tenant_id=p_tenant AND s.project_id=profile_row.project_id
   AND s.id=ANY(p_sessions);
 IF selected_count<>cardinality(p_sessions) THEN
  RAISE EXCEPTION 'V23_TRAIN_SESSION_SCOPE' USING ERRCODE='23514';
 END IF;
 SELECT count(*) INTO candidate_count FROM photo.correction_events e
  WHERE e.tenant_id=p_tenant AND e.project_id=profile_row.project_id
   AND e.session_id=ANY(p_sessions) AND e.learn
   AND e.person_id IS NOT DISTINCT FROM profile_row.person_id;
 IF candidate_count>5000 THEN
  RAISE EXCEPTION 'V23_TRAIN_TOO_MANY_EVENTS' USING ERRCODE='22023';
 END IF;
 RETURN QUERY
 SELECT e.id,e.session_id,e.asset_version_id,v.content_hash,
  original.sha256,base.id,base.artifact_id,base.sha256,
  approved.id,approved.artifact_id,approved.sha256,
  approval.id,e.created_at,e.consent_id,e.consent_epoch,e.changed_paths,
  (c.id IS NOT NULL AND c.status='granted' AND c.epoch=e.consent_epoch
   AND c.person_id IS NOT DISTINCT FROM profile_row.person_id
   AND c.purpose=CASE WHEN profile_row.person_id IS NULL THEN
    'learning_photographer' ELSE 'learning_person' END
   AND (profile_row.person_id IS NULL OR EXISTS(SELECT 1
    FROM photo.project_persons person WHERE person.tenant_id=p_tenant
    AND person.project_id=profile_row.project_id AND
    person.id=profile_row.person_id AND person.state='active'))),
  (v.review_state='accepted' AND v.created_by_run_id IS NULL
   AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND original.state='ready' AND original.verified_at IS NOT NULL
   AND original.object_version IS NOT NULL AND
   original.object_version NOT IN('','null')
   AND (original.expires_at IS NULL OR
    original.expires_at>clock_timestamp())
   AND master.state='ready' AND master.verified_at IS NOT NULL
   AND master.object_version IS NOT NULL AND master.object_version NOT IN('','null')
   AND (master.expires_at IS NULL OR
    master.expires_at>clock_timestamp())),
  (base_artifact.state='ready' AND base_artifact.verified_at IS NOT NULL
   AND base_artifact.object_version IS NOT NULL AND
   base_artifact.object_version NOT IN('','null') AND
   (base_artifact.expires_at IS NULL OR
    base_artifact.expires_at>clock_timestamp())),
  (approval.id IS NOT NULL AND approval.recipe_sha=approved.sha256
   AND resolved.state='ready' AND resolved.verified_at IS NOT NULL
   AND resolved.object_version IS NOT NULL AND
   resolved.object_version NOT IN('','null')
   AND (resolved.expires_at IS NULL OR
    resolved.expires_at>clock_timestamp())
   AND approved_artifact.state='ready' AND approved_artifact.verified_at IS NOT NULL
   AND approved_artifact.object_version IS NOT NULL AND
   approved_artifact.object_version NOT IN('','null') AND
   (approved_artifact.expires_at IS NULL OR
    approved_artifact.expires_at>clock_timestamp()))
 FROM photo.correction_events e
 JOIN photo.asset_versions v ON v.tenant_id=e.tenant_id
  AND v.id=e.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts original ON original.tenant_id=asset.tenant_id
  AND original.id=asset.original_artifact_id
 JOIN photo.artifacts master ON master.tenant_id=v.tenant_id
  AND master.id=v.master_artifact_id
 JOIN photo.v2_documents base ON base.tenant_id=e.tenant_id
  AND base.id=e.base_document_id AND base.project_id=e.project_id
 JOIN photo.v2_documents approved ON approved.tenant_id=e.tenant_id
  AND approved.id=e.approved_document_id AND approved.project_id=e.project_id
 LEFT JOIN photo.v2_document_approvals approval ON approval.tenant_id=e.tenant_id
  AND approval.project_id=e.project_id AND approval.document_id=approved.id
 LEFT JOIN photo.artifacts resolved ON resolved.tenant_id=approval.tenant_id
  AND resolved.id=approval.resolved_artifact_id
 JOIN photo.artifacts base_artifact ON base_artifact.tenant_id=base.tenant_id
  AND base_artifact.id=base.artifact_id AND base_artifact.sha256=base.sha256
 JOIN photo.artifacts approved_artifact ON approved_artifact.tenant_id=approved.tenant_id
  AND approved_artifact.id=approved.artifact_id AND
  approved_artifact.sha256=approved.sha256
 LEFT JOIN photo.consent_grants c ON c.tenant_id=e.tenant_id
  AND c.project_id=e.project_id AND c.id=e.consent_id
 WHERE e.tenant_id=p_tenant AND e.project_id=profile_row.project_id
  AND e.session_id=ANY(p_sessions) AND e.learn
  AND e.person_id IS NOT DISTINCT FROM profile_row.person_id
 ORDER BY e.created_at,e.id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_training_candidate_rows(uuid,uuid,bigint,uuid[],uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_training_candidate_rows(uuid,uuid,bigint,uuid[],uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_training_candidate_rows(uuid,uuid,bigint,uuid[],uuid,uuid) TO photo_api;
