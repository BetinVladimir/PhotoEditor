-- Read a decided defect review with its canonical proposal and completed
-- repair Params. This does not establish correction-event/source-version
-- identity; that association must be checked by the later holdout publisher.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_reviewed_defect_source(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_review uuid)
RETURNS TABLE(review_id uuid,run_id uuid,actor_id uuid,decision text,
 route text,decision_hash text,revision bigint,selection jsonb,
 image_id uuid,image_sha text,proposal_id uuid,proposal_sha text,
 labels_id uuid,labels_sha text,selection_id uuid,selection_sha text,
 resolved_id uuid,resolved_sha text,params_id uuid,params_sha text,
 proposal_task_id uuid,repair_task_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_run uuid; source_task uuid;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_review IS NULL THEN RETURN; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,
  p_actor,p_auth_session) THEN RETURN; END IF;
 SELECT v.run_id,v.task_id INTO source_run,source_task FROM photo.reviews v
  JOIN photo.v2_workflows w ON w.tenant_id=v.tenant_id AND w.run_id=v.run_id
  WHERE v.tenant_id=p_tenant AND v.id=p_review AND w.project_id=p_project;
 IF source_run IS NULL OR source_task IS NULL THEN RETURN; END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=source_run
  AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.reviews WHERE tenant_id=p_tenant AND id=p_review
  AND run_id=source_run AND task_id=source_task AND kind='defects'
  AND state='decided' AND decision='accept' AND decision_route='repair'
  FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 RETURN QUERY
 SELECT v.id,v.run_id,v.decided_by,v.decision,v.decision_route,
  v.decision_hash,v.revision,v.selection,
  image.id,image.sha256,candidates.id,candidates.sha256,
  labels.id,labels.sha256,selected.id,selected.sha256,
  resolved.id,resolved.sha256,params.id,params.sha256,proposal.id,repair.id
 FROM photo.reviews v
 JOIN photo.tasks proposal ON proposal.tenant_id=v.tenant_id
  AND proposal.id=v.task_id AND proposal.run_id=v.run_id
  AND proposal.operation='defects.propose' AND proposal.operation_version='1'
  AND proposal.state='succeeded' AND proposal.canonical_result_id IS NOT NULL
 JOIN photo.task_results proposal_result ON
  proposal_result.tenant_id=proposal.tenant_id
  AND proposal_result.id=proposal.canonical_result_id
  AND proposal_result.task_id=proposal.id
 JOIN photo.artifacts image ON image.tenant_id=v.tenant_id
  AND image.id=v.base_artifact_id AND image.state='ready'
  AND image.verified_at IS NOT NULL AND image.object_version IS NOT NULL
  AND image.object_version NOT IN ('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
  AND proposal.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',image.id::text,'sha256',image.sha256))
 JOIN photo.artifacts candidates ON candidates.tenant_id=v.tenant_id
  AND candidates.id=v.candidate_artifact_id
  AND candidates.sha256=v.candidate_hash AND candidates.state='ready'
  AND candidates.created_by_task_id=proposal.id
  AND candidates.output_role='defect_candidates'
  AND candidates.verified_at IS NOT NULL
  AND candidates.object_version IS NOT NULL
  AND candidates.object_version NOT IN ('','null')
  AND (candidates.expires_at IS NULL OR candidates.expires_at>clock_timestamp())
  AND proposal_result.outputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',candidates.id::text,'sha256',candidates.sha256))
 JOIN photo.artifacts labels ON labels.tenant_id=proposal.tenant_id
  AND labels.created_by_task_id=proposal.id
  AND labels.output_role='defect_labels' AND labels.state='ready'
  AND labels.verified_at IS NOT NULL AND labels.object_version IS NOT NULL
  AND labels.object_version NOT IN ('','null')
  AND (labels.expires_at IS NULL OR labels.expires_at>clock_timestamp())
  AND proposal_result.outputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',labels.id::text,'sha256',labels.sha256))
 JOIN photo.defect_branches branch ON branch.tenant_id=v.tenant_id
  AND branch.run_id=v.run_id AND branch.review_id=v.id
  AND branch.task_id IS NOT NULL
 JOIN photo.backend_documents selected_doc ON
  selected_doc.tenant_id=v.tenant_id AND selected_doc.run_id=v.run_id
  AND selected_doc.owner_id=v.id AND selected_doc.purpose='review_selection'
  AND selected_doc.state='ready'
 JOIN photo.artifacts selected ON selected.tenant_id=selected_doc.tenant_id
  AND selected.id=selected_doc.artifact_id
  AND selected.sha256=selected_doc.sha256 AND selected.state='ready'
  AND selected.object_version=selected_doc.object_version
  AND selected.verified_at IS NOT NULL
  AND (selected.expires_at IS NULL OR selected.expires_at>clock_timestamp())
 JOIN photo.artifacts resolved ON resolved.tenant_id=v.tenant_id
  AND resolved.id=v.resolved_artifact_id AND resolved.state='ready'
  AND resolved.output_role='resolved_style'
  AND resolved.verified_at IS NOT NULL AND resolved.object_version IS NOT NULL
  AND resolved.object_version NOT IN ('','null')
  AND (resolved.expires_at IS NULL OR resolved.expires_at>clock_timestamp())
 JOIN photo.tasks repair ON repair.tenant_id=branch.tenant_id
  AND repair.id=branch.task_id AND repair.run_id=branch.run_id
  AND repair.operation='defects.repair' AND repair.operation_version='1'
  AND repair.state='succeeded' AND repair.canonical_result_id IS NOT NULL
 JOIN photo.artifacts params ON params.tenant_id=repair.tenant_id
  AND params.id=repair.params_artifact_id AND params.state='ready'
  AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL
  AND params.object_version NOT IN ('','null')
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 JOIN photo.backend_documents params_doc ON
  params_doc.tenant_id=repair.tenant_id AND params_doc.run_id=repair.run_id
  AND params_doc.artifact_id=params.id AND params_doc.purpose='task_params'
  AND params_doc.state='ready' AND params_doc.sha256=params.sha256
  AND params_doc.object_version=params.object_version
 WHERE v.tenant_id=p_tenant AND v.id=p_review AND v.run_id=source_run
  AND v.task_id=source_task AND v.kind='defects' AND v.state='decided'
  AND v.decision='accept' AND v.decision_route='repair'
  AND v.decided_by IS NOT NULL AND v.decision_hash ~ '^[0-9a-f]{64}$'
  AND v.revision BETWEEN 1 AND 9007199254740991
  AND jsonb_typeof(v.selection)='object'
  AND (v.selection->'image'->>'artifact_id')::uuid=image.id
  AND v.selection->'image'->>'sha256'=image.sha256
  AND (v.selection->'proposal'->>'artifact_id')::uuid=candidates.id
  AND v.selection->'proposal'->>'sha256'=candidates.sha256
  AND (v.selection->'label_map'->>'artifact_id')::uuid=labels.id
  AND v.selection->'label_map'->>'sha256'=labels.sha256
  AND repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',image.id::text,'sha256',image.sha256))
  AND repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',candidates.id::text,'sha256',candidates.sha256))
  AND repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',selected.id::text,'sha256',selected.sha256))
  AND repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',resolved.id::text,'sha256',resolved.sha256))
  AND params_doc.owner_id=branch.expansion_id;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_defect_source(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_defect_source(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_reviewed_defect_source(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
