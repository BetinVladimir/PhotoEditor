-- Derive one reviewed defect source for a training correction event. An
-- ambiguous source is refused; callers cannot nominate a review by ID.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_reviewed_defect_event_source(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_event uuid)
RETURNS TABLE(review_id uuid,asset_version_id uuid,source_id uuid,
 source_sha text,consent_id uuid,consent_epoch bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE event_row photo.correction_events%ROWTYPE;
 version_row photo.asset_versions%ROWTYPE;
 matched_review uuid; match_count bigint; current_source record;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL THEN RETURN; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,
  p_actor,p_auth_session) THEN RETURN; END IF;
 SELECT * INTO event_row FROM photo.correction_events e
  WHERE e.tenant_id=p_tenant AND e.project_id=p_project AND e.id=p_event
   AND e.learn AND e.consent_id IS NOT NULL AND e.consent_epoch IS NOT NULL
  FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT v.* INTO version_row FROM photo.asset_versions v
  JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
  WHERE v.tenant_id=p_tenant AND v.id=event_row.asset_version_id
   AND a.project_id=p_project AND a.deleted_at IS NULL
   AND a.upload_state='verified' AND v.review_state='accepted'
   AND v.created_by_run_id IS NULL FOR SHARE OF v;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.consent_grants g WHERE g.tenant_id=p_tenant
  AND g.project_id=p_project AND g.id=event_row.consent_id
  AND g.epoch=event_row.consent_epoch AND g.status='granted'
  AND g.person_id IS NOT DISTINCT FROM event_row.person_id
  AND g.purpose=CASE WHEN event_row.person_id IS NULL THEN
   'learning_photographer' ELSE 'learning_person' END FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.artifacts image WHERE image.tenant_id=p_tenant
  AND image.id=version_row.master_artifact_id
  AND image.sha256=version_row.content_hash AND image.state='ready'
  AND image.verified_at IS NOT NULL AND image.object_version IS NOT NULL
  AND image.object_version NOT IN ('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
  FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT count(*),min(v.id::text)::uuid INTO match_count,matched_review
 FROM photo.reviews v
 JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
 JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 JOIN photo.tasks proposal ON proposal.tenant_id=v.tenant_id
  AND proposal.id=v.task_id AND proposal.run_id=v.run_id
 JOIN photo.defect_branches branch ON branch.tenant_id=v.tenant_id
  AND branch.run_id=v.run_id AND branch.review_id=v.id
 JOIN photo.tasks repair ON repair.tenant_id=branch.tenant_id
  AND repair.id=branch.task_id AND repair.run_id=branch.run_id
 WHERE v.tenant_id=p_tenant AND w.project_id=p_project
  AND v.kind='defects' AND v.state='decided' AND v.decision='accept'
  AND v.decision_route='repair' AND v.decided_by=event_row.approved_by
  AND v.decided_at IS NOT NULL AND v.decided_at<=event_row.created_at
  AND v.base_artifact_id=version_row.master_artifact_id
  AND NOT r.cancel_requested AND proposal.operation='defects.propose'
  AND proposal.operation_version='1' AND proposal.state='succeeded'
  AND proposal.canonical_result_id IS NOT NULL
  AND repair.operation='defects.repair' AND repair.operation_version='1'
  AND repair.state='succeeded' AND repair.canonical_result_id IS NOT NULL;
 IF match_count<>1 OR matched_review IS NULL THEN RETURN; END IF;
 SELECT * INTO current_source FROM photo.v23_reviewed_defect_source(
  p_tenant,p_project,p_actor,p_auth_session,matched_review);
 IF NOT FOUND OR current_source.image_id<>version_row.master_artifact_id OR
  current_source.image_sha<>version_row.content_hash OR
  current_source.actor_id<>event_row.approved_by THEN RETURN; END IF;
 RETURN QUERY SELECT matched_review,event_row.asset_version_id,
  version_row.master_artifact_id,version_row.content_hash,
  event_row.consent_id,event_row.consent_epoch;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_defect_event_source(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_defect_event_source(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_reviewed_defect_event_source(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
