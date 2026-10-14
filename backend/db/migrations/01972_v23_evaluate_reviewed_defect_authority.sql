-- Evaluate derives the current review from the frozen event and source version.
-- No editor session or caller-selected review ID is accepted at this boundary.
-- The caller repeats this lookup after exact-version object IO.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_reviewed_defect_authority(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,
 p_event uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(review_id uuid,review_run_id uuid,actor_id uuid,
 decision_hash text,asset_version_id uuid,image_id uuid,image_sha text,
 params_id uuid,params_sha text,proposal_id uuid,proposal_sha text,
 labels_id uuid,labels_sha text,selection_id uuid,selection_sha text,
 resolved_id uuid,resolved_sha text,
 bucket text,object_key text,object_version text,bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.correction_events%ROWTYPE; v photo.asset_versions%ROWTYPE;
 d photo.backend_documents%ROWTYPE; root record; project_id uuid;
 matched bigint; candidate record; h bytea; expected_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_event IS NULL OR p_artifact IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 h:=public.digest(uuid_send(p_run)||convert_to(
  'v23-defect/'||p_event::text,'UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_artifact<>expected_id THEN RETURN; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO root FROM photo.v23_evaluate_frozen_root(p_tenant,p_run,
  p_expansion,p_token,p_artifact,p_sha,'FrozenDefectHoldoutEvidence');
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=p_event AND
  x.purpose='v23_defect_holdout' AND x.artifact_id=p_artifact AND
  x.sha256=p_sha AND x.state='ready' AND
  x.object_version=root.object_version AND x.bucket=root.bucket AND
  x.bytes=root.bytes;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO e FROM photo.correction_events x WHERE x.tenant_id=p_tenant
  AND x.id=p_event AND x.learn AND x.consent_id IS NOT NULL AND
  x.consent_epoch IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT * INTO v FROM photo.asset_versions x JOIN photo.assets a
  ON a.tenant_id=x.tenant_id AND a.id=x.asset_id
  WHERE x.tenant_id=p_tenant AND x.id=e.asset_version_id AND
   a.project_id=e.project_id AND a.deleted_at IS NULL AND
   a.upload_state='verified' AND x.review_state='accepted' AND
   x.created_by_run_id IS NULL FOR SHARE OF x;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.consent_grants g WHERE g.tenant_id=p_tenant AND
  g.project_id=e.project_id AND g.id=e.consent_id AND
  g.epoch=e.consent_epoch AND g.status='granted' AND
  g.person_id IS NOT DISTINCT FROM e.person_id AND
  g.purpose=CASE WHEN e.person_id IS NULL THEN
   'learning_photographer' ELSE 'learning_person' END FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
  a.id=v.master_artifact_id AND a.sha256=v.content_hash AND
  a.state='ready' AND a.verified_at IS NOT NULL AND
  a.object_version IS NOT NULL AND a.object_version NOT IN ('','null') AND
  (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT count(*),min(p.project_id::text)::uuid INTO matched,project_id
 FROM photo.runs r JOIN photo.learning_profiles p
  ON p.tenant_id=r.tenant_id AND p.id=NULLIF(
   r.config_snapshot->'v23_training'->>'profile_id','')::uuid
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND
  NOT r.cancel_requested AND p.project_id=e.project_id AND
  r.created_by=e.approved_by;
 IF matched<>1 THEN RETURN; END IF;
 SELECT count(*) INTO matched FROM photo.reviews rev
 JOIN photo.v2_workflows w ON w.tenant_id=rev.tenant_id AND
  w.run_id=rev.run_id AND w.project_id=project_id
 JOIN photo.runs source_run ON source_run.tenant_id=rev.tenant_id AND
  source_run.id=rev.run_id AND NOT source_run.cancel_requested
 JOIN photo.tasks proposal ON proposal.tenant_id=rev.tenant_id AND
  proposal.id=rev.task_id AND proposal.run_id=rev.run_id AND
  proposal.operation='defects.propose' AND proposal.operation_version='1'
  AND proposal.state='succeeded' AND proposal.canonical_result_id IS NOT NULL
 JOIN photo.defect_branches branch ON branch.tenant_id=rev.tenant_id AND
  branch.run_id=rev.run_id AND branch.review_id=rev.id
 JOIN photo.tasks repair ON repair.tenant_id=branch.tenant_id AND
  repair.id=branch.task_id AND repair.run_id=branch.run_id AND
  repair.operation='defects.repair' AND repair.operation_version='1' AND
  repair.state='succeeded' AND repair.canonical_result_id IS NOT NULL
 WHERE rev.tenant_id=p_tenant AND rev.kind='defects' AND
  rev.state='decided' AND rev.decision='accept' AND
  rev.decision_route='repair' AND rev.decided_by=e.approved_by AND
  rev.decided_at IS NOT NULL AND rev.decided_at<=e.created_at AND
  rev.base_artifact_id=v.master_artifact_id;
 IF matched<>1 THEN RETURN; END IF;
 -- The exact selected review and repair Params are returned only after the
 -- same producer and source predicates have proved uniqueness above. Artifact
 -- joins must themselves resolve to exactly one row, never an arbitrary first.
 BEGIN
 SELECT rev.id AS review_id,rev.run_id,rev.decided_by,
  rev.decision_hash,repair.params_artifact_id,params.sha256,
  candidates.id AS proposal_id,candidates.sha256 AS proposal_sha,
  labels.id AS labels_id,labels.sha256 AS labels_sha,
  selected.id AS selection_id,selected.sha256 AS selection_sha,
  resolved.id AS resolved_id,resolved.sha256 AS resolved_sha
 INTO STRICT candidate FROM photo.reviews rev
 JOIN photo.v2_workflows w ON w.tenant_id=rev.tenant_id AND
  w.run_id=rev.run_id AND w.project_id=project_id
 JOIN photo.runs source_run ON source_run.tenant_id=rev.tenant_id AND
  source_run.id=rev.run_id AND NOT source_run.cancel_requested
 JOIN photo.tasks proposal ON proposal.tenant_id=rev.tenant_id AND
  proposal.id=rev.task_id AND proposal.run_id=rev.run_id AND
  proposal.operation='defects.propose' AND proposal.operation_version='1' AND
  proposal.state='succeeded' AND proposal.canonical_result_id IS NOT NULL
 JOIN photo.task_results proposal_result ON
  proposal_result.tenant_id=proposal.tenant_id AND
  proposal_result.id=proposal.canonical_result_id AND
  proposal_result.task_id=proposal.id
 JOIN photo.artifacts candidates ON candidates.tenant_id=rev.tenant_id AND
  candidates.id=rev.candidate_artifact_id AND
  candidates.sha256=rev.candidate_hash AND candidates.state='ready' AND
  candidates.created_by_task_id=proposal.id AND
  candidates.output_role='defect_candidates' AND
  candidates.verified_at IS NOT NULL AND
  candidates.object_version IS NOT NULL AND
  candidates.object_version NOT IN ('','null') AND
  (candidates.expires_at IS NULL OR candidates.expires_at>clock_timestamp())
 JOIN photo.artifacts labels ON labels.tenant_id=proposal.tenant_id AND
  labels.created_by_task_id=proposal.id AND
  labels.output_role='defect_labels' AND labels.state='ready' AND
  labels.verified_at IS NOT NULL AND labels.object_version IS NOT NULL AND
  labels.object_version NOT IN ('','null') AND
  (labels.expires_at IS NULL OR labels.expires_at>clock_timestamp())
 JOIN photo.defect_branches branch ON branch.tenant_id=rev.tenant_id AND
  branch.run_id=rev.run_id AND branch.review_id=rev.id
 JOIN photo.backend_documents selected_doc ON
  selected_doc.tenant_id=rev.tenant_id AND
  selected_doc.run_id=rev.run_id AND selected_doc.owner_id=rev.id AND
  selected_doc.purpose='review_selection' AND selected_doc.state='ready'
 JOIN photo.artifacts selected ON selected.tenant_id=selected_doc.tenant_id
  AND selected.id=selected_doc.artifact_id AND
  selected.sha256=selected_doc.sha256 AND selected.state='ready' AND
  selected.object_version=selected_doc.object_version AND
  selected.verified_at IS NOT NULL AND
  (selected.expires_at IS NULL OR selected.expires_at>clock_timestamp())
 JOIN photo.artifacts resolved ON resolved.tenant_id=rev.tenant_id AND
  resolved.id=rev.resolved_artifact_id AND resolved.state='ready' AND
  resolved.output_role='resolved_style' AND resolved.verified_at IS NOT NULL
  AND resolved.object_version IS NOT NULL AND
  resolved.object_version NOT IN ('','null') AND
  (resolved.expires_at IS NULL OR resolved.expires_at>clock_timestamp())
 JOIN photo.tasks repair ON repair.tenant_id=branch.tenant_id AND
  repair.id=branch.task_id AND repair.run_id=branch.run_id AND
  repair.operation='defects.repair' AND repair.operation_version='1' AND
  repair.state='succeeded' AND repair.canonical_result_id IS NOT NULL
 JOIN photo.artifacts params ON params.tenant_id=repair.tenant_id AND
  params.id=repair.params_artifact_id AND params.state='ready' AND
  params.verified_at IS NOT NULL AND params.object_version IS NOT NULL AND
  params.object_version NOT IN ('','null') AND
  (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 JOIN photo.backend_documents pd ON pd.tenant_id=repair.tenant_id AND
  pd.run_id=repair.run_id AND pd.artifact_id=params.id AND
  pd.sha256=params.sha256 AND pd.purpose='task_params' AND
  pd.state='ready' AND pd.object_version=params.object_version AND
  pd.owner_id=branch.expansion_id
 WHERE rev.tenant_id=p_tenant AND rev.kind='defects' AND
  rev.state='decided' AND rev.decision='accept' AND
  rev.decision_route='repair' AND rev.decided_by=e.approved_by AND
  rev.decided_at IS NOT NULL AND rev.decided_at<=e.created_at AND
  rev.base_artifact_id=v.master_artifact_id AND
  rev.revision BETWEEN 1 AND 9007199254740991 AND
  jsonb_typeof(rev.selection)='object' AND
  (rev.selection->'image'->>'artifact_id')::uuid=v.master_artifact_id AND
  rev.selection->'image'->>'sha256'=v.content_hash AND
  (rev.selection->'proposal'->>'artifact_id')::uuid=candidates.id AND
  rev.selection->'proposal'->>'sha256'=candidates.sha256 AND
  (rev.selection->'label_map'->>'artifact_id')::uuid=labels.id AND
  rev.selection->'label_map'->>'sha256'=labels.sha256 AND
  proposal.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',v.master_artifact_id::text,'sha256',v.content_hash)) AND
  proposal_result.outputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',candidates.id::text,'sha256',candidates.sha256)) AND
  proposal_result.outputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',labels.id::text,'sha256',labels.sha256)) AND
  repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',v.master_artifact_id::text,'sha256',v.content_hash)) AND
  repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',candidates.id::text,'sha256',candidates.sha256)) AND
  repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',selected.id::text,'sha256',selected.sha256)) AND
  repair.inputs @> jsonb_build_array(jsonb_build_object(
   'artifact_id',resolved.id::text,'sha256',resolved.sha256))
 FOR SHARE OF rev;
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN;
 END;
 IF candidate.decision_hash !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 RETURN QUERY SELECT candidate.review_id,candidate.run_id,
  candidate.decided_by,candidate.decision_hash,e.asset_version_id,
  v.master_artifact_id,v.content_hash,candidate.params_artifact_id,
  candidate.sha256,candidate.proposal_id,candidate.proposal_sha,
  candidate.labels_id,candidate.labels_sha,candidate.selection_id,
  candidate.selection_sha,candidate.resolved_id,candidate.resolved_sha,
  root.bucket,root.object_key,root.object_version,root.bytes;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_reviewed_defect_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_reviewed_defect_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_reviewed_defect_authority(
 uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
