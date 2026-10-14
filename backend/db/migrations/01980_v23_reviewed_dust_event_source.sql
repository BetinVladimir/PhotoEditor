-- Read one reviewed sensor-dust Apply v4 source for a learning event.
-- The source image and the color-stage image are distinct immutable objects.
-- This function grants no publication or Evaluate task authority.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_reviewed_dust_event_source(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_event uuid)
RETURNS TABLE(review_id uuid,review_run_id uuid,actor_id uuid,
 decision_hash text,revision bigint,selection jsonb,
 asset_version_id uuid,consent_id uuid,consent_epoch bigint,
 image_id uuid,image_sha text,
 stage_image_id uuid,stage_image_sha text,
 proposal_id uuid,proposal_sha text,params_id uuid,params_sha text,
 detect_task_id uuid,repair_task_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.correction_events%ROWTYPE;
 v photo.asset_versions%ROWTYPE; source_row record;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL THEN RETURN; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,
  p_actor,p_auth_session) THEN RETURN; END IF;
 SELECT * INTO e FROM photo.correction_events x WHERE x.tenant_id=p_tenant
  AND x.project_id=p_project AND x.id=p_event AND x.learn AND
  x.consent_id IS NOT NULL AND x.consent_epoch IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT x.* INTO v FROM photo.asset_versions x JOIN photo.assets a
  ON a.tenant_id=x.tenant_id AND a.id=x.asset_id
  WHERE x.tenant_id=p_tenant AND x.id=e.asset_version_id AND
   a.project_id=p_project AND a.deleted_at IS NULL AND
   a.upload_state='verified' AND x.review_state='accepted' AND
   x.created_by_run_id IS NULL FOR SHARE OF x;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.consent_grants g WHERE g.tenant_id=p_tenant AND
  g.project_id=p_project AND g.id=e.consent_id AND
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

 -- STRICT prevents an arbitrary first review when the same correction has
 -- more than one eligible Apply path. Every joined artifact is versioned and
 -- attached to its canonical producer result or task Params document.
 BEGIN
  SELECT rev.id AS review_id,rev.run_id AS review_run_id,
   rev.decided_by AS actor_id,rev.decision_hash,rev.revision,rev.selection,
   stage.id AS stage_image_id,stage.sha256 AS stage_image_sha,
   proposal.id AS proposal_id,proposal.sha256 AS proposal_sha,
   params.id AS params_id,params.sha256 AS params_sha,
   detect.id AS detect_task_id,repair.id AS repair_task_id
  INTO STRICT source_row
  FROM photo.runs r
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
   AND w.project_id=p_project
  JOIN photo.apply_builds build ON build.tenant_id=r.tenant_id AND
   build.run_id=r.id AND build.graph_version=4
  JOIN photo.snapshot_items item ON item.tenant_id=r.tenant_id AND
   item.snapshot_id=r.input_snapshot_id AND
   item.asset_version_id=e.asset_version_id AND
   item.artifact_id=v.master_artifact_id AND item.sha256=v.content_hash
  JOIN photo.apply_nodes color_node ON color_node.tenant_id=r.tenant_id AND
   color_node.run_id=r.id AND color_node.phase='color' AND
   color_node.operation='image.color' AND
   color_node.source_item_id=item.item_id AND
   color_node.source_version_id=e.asset_version_id
  JOIN photo.tasks color_task ON color_task.tenant_id=color_node.tenant_id AND
   color_task.id=color_node.id AND color_task.run_id=r.id AND
   color_task.state='succeeded' AND color_task.canonical_result_id IS NOT NULL
  JOIN photo.task_results color_result ON
   color_result.tenant_id=color_task.tenant_id AND
   color_result.id=color_task.canonical_result_id AND
   color_result.task_id=color_task.id
  JOIN photo.artifacts stage ON stage.tenant_id=color_task.tenant_id AND
   stage.created_by_task_id=color_task.id AND
   stage.produced_attempt=color_result.attempt AND
   stage.produced_fence=color_result.fencing_token AND
   stage.output_role='working_image' AND stage.media_type='application/json' AND
   stage.state='ready' AND stage.verified_at IS NOT NULL AND
   stage.object_version IS NOT NULL AND stage.object_version NOT IN ('','null') AND
   (stage.expires_at IS NULL OR stage.expires_at>clock_timestamp()) AND
   color_result.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256))
  JOIN photo.apply_nodes repair_node ON repair_node.tenant_id=r.tenant_id AND
   repair_node.run_id=r.id AND repair_node.phase='dust_repair' AND
   repair_node.operation='sensor.dust_repair' AND
   repair_node.source_item_id=item.item_id AND
   repair_node.source_version_id=e.asset_version_id AND
   repair_node.params->'image'=jsonb_build_object(
    'artifact_id',stage.id,'sha256',stage.sha256)
  JOIN photo.tasks repair ON repair.tenant_id=repair_node.tenant_id AND
   repair.id=repair_node.id AND repair.run_id=r.id AND
   repair.operation='sensor.dust_repair' AND repair.operation_version='1' AND
   repair.state='succeeded' AND repair.canonical_result_id IS NOT NULL AND
   repair.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256))
  JOIN photo.apply_nodes detect_node ON detect_node.tenant_id=r.tenant_id AND
   detect_node.run_id=r.id AND detect_node.phase='dust_detect' AND
   detect_node.operation='sensor.dust' AND
   detect_node.id=ANY(repair_node.prerequisites) AND
   detect_node.params->'frames' @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id,'sha256',stage.sha256))
  JOIN photo.tasks detect ON detect.tenant_id=detect_node.tenant_id AND
   detect.id=detect_node.id AND detect.run_id=r.id AND
   detect.operation='sensor.dust' AND detect.operation_version='1' AND
   detect.state='succeeded' AND detect.canonical_result_id IS NOT NULL AND
   detect.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256))
  JOIN photo.task_results detect_result ON
   detect_result.tenant_id=detect.tenant_id AND
   detect_result.id=detect.canonical_result_id AND
   detect_result.task_id=detect.id
  JOIN photo.reviews rev ON rev.tenant_id=detect.tenant_id AND
   rev.run_id=r.id AND rev.task_id=detect.id AND
   rev.kind='sensor_dust' AND rev.state='decided' AND
   rev.decision='accept' AND rev.decision_route='repair' AND
   rev.decided_by=e.approved_by AND rev.decided_at IS NOT NULL AND
   rev.decided_at<=e.created_at AND rev.revision BETWEEN 1 AND 9007199254740991
  JOIN photo.artifacts base ON base.tenant_id=rev.tenant_id AND
   base.id=rev.base_artifact_id AND base.state='ready' AND
   base.verified_at IS NOT NULL AND base.object_version IS NOT NULL AND
   base.object_version NOT IN ('','null') AND
   (base.expires_at IS NULL OR base.expires_at>clock_timestamp()) AND
   detect.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',base.id::text,'sha256',base.sha256))
  JOIN photo.artifacts proposal ON proposal.tenant_id=rev.tenant_id AND
   proposal.id=rev.candidate_artifact_id AND
   proposal.sha256=rev.candidate_hash AND
   proposal.created_by_task_id=detect.id AND
   proposal.produced_attempt=detect_result.attempt AND
   proposal.produced_fence=detect_result.fencing_token AND
   proposal.output_role='dust_candidates' AND proposal.state='ready' AND
   proposal.verified_at IS NOT NULL AND proposal.object_version IS NOT NULL AND
   proposal.object_version NOT IN ('','null') AND
   (proposal.expires_at IS NULL OR proposal.expires_at>clock_timestamp()) AND
   detect_result.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',proposal.id::text,'sha256',proposal.sha256)) AND
   repair.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',proposal.id::text,'sha256',proposal.sha256))
  JOIN photo.artifacts params ON params.tenant_id=repair.tenant_id AND
   params.id=repair.params_artifact_id AND params.sha256=repair_node.params_sha256 AND
   params.state='ready' AND params.verified_at IS NOT NULL AND
   params.object_version IS NOT NULL AND params.object_version NOT IN ('','null') AND
   (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
  JOIN photo.backend_documents params_doc ON params_doc.tenant_id=repair.tenant_id AND
   params_doc.run_id=r.id AND params_doc.owner_id=repair.id AND
   params_doc.purpose='task_params' AND params_doc.state='ready' AND
   params_doc.artifact_id=params.id AND params_doc.sha256=params.sha256 AND
   params_doc.object_version=params.object_version
  WHERE r.tenant_id=p_tenant AND NOT r.cancel_requested AND
   rev.decision_hash ~ '^[0-9a-f]{64}$' AND
   jsonb_typeof(rev.selection)='object' AND
   (rev.selection->>'revision')::bigint=rev.revision AND
   rev.selection->'proposal'=jsonb_build_object(
    'artifact_id',proposal.id,'sha256',proposal.sha256) AND
   rev.selection->'base'=jsonb_build_object(
    'artifact_id',base.id,'sha256',base.sha256) AND
   repair_node.params->'proposal'=jsonb_build_object(
    'artifact_id',proposal.id,'sha256',proposal.sha256) AND
   repair_node.params->'review'=rev.selection;
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN;
 END;
 RETURN QUERY SELECT source_row.review_id,source_row.review_run_id,
  source_row.actor_id,source_row.decision_hash,source_row.revision,
  source_row.selection,e.asset_version_id,e.consent_id,e.consent_epoch,
  v.master_artifact_id,
  v.content_hash,source_row.stage_image_id,source_row.stage_image_sha,
  source_row.proposal_id,source_row.proposal_sha,source_row.params_id,
  source_row.params_sha,source_row.detect_task_id,source_row.repair_task_id;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_reviewed_dust_event_source(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_reviewed_dust_event_source(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_reviewed_dust_event_source(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
