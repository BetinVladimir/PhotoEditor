-- Select a unique successful Apply v4 portrait.detail producer candidate for
-- one learnable correction. An explicit event association is still required
-- before publication. This read grants neither object access nor publication.
-- The returned image is the detail-stage input, distinct from the original
-- asset-version source. Mask/protection documents need separate exact closure.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_portrait_detail_event_producer(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_event uuid)
RETURNS TABLE(asset_version_id uuid,source_id uuid,source_sha text,
 consent_id uuid,consent_epoch bigint,run_id uuid,task_id uuid,
 result_id uuid,attempt_id uuid,image_id uuid,image_sha text,
 params_id uuid,params_sha text,resolved_id uuid,resolved_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.correction_events%ROWTYPE;
 v photo.asset_versions%ROWTYPE;
 producer record;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL THEN RETURN; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND OR NOT photo.v2_editor_authorized(p_tenant,p_project,
  p_actor,p_auth_session) THEN RETURN; END IF;
 SELECT * INTO e FROM photo.correction_events x WHERE x.tenant_id=p_tenant
  AND x.project_id=p_project AND x.id=p_event AND x.learn
  AND x.consent_id IS NOT NULL AND x.consent_epoch IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 SELECT x.* INTO v FROM photo.asset_versions x JOIN photo.assets a
  ON a.tenant_id=x.tenant_id AND a.id=x.asset_id
  WHERE x.tenant_id=p_tenant AND x.id=e.asset_version_id
   AND a.project_id=p_project AND a.deleted_at IS NULL
   AND a.upload_state='verified' AND x.review_state='accepted'
   AND x.created_by_run_id IS NULL FOR SHARE OF x;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.consent_grants g WHERE g.tenant_id=p_tenant
  AND g.project_id=p_project AND g.id=e.consent_id
  AND g.epoch=e.consent_epoch AND g.status='granted'
  AND g.person_id IS NOT DISTINCT FROM e.person_id
  AND g.purpose=CASE WHEN e.person_id IS NULL THEN
   'learning_photographer' ELSE 'learning_person' END FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;
 PERFORM id FROM photo.artifacts a WHERE a.tenant_id=p_tenant
  AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
  AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;

 -- STRICT refuses both absent and ambiguous producer paths. A matching SHA
 -- alone cannot nominate a producer: every stage belongs to the same Apply
 -- snapshot item, and the stage input and resolved style are canonical
 -- outputs of its declared prerequisite tasks.
 BEGIN
  SELECT r.id AS run_id,detail.id AS task_id,dr.id AS result_id,
   da.id AS attempt_id,img.id AS image_id,img.sha256 AS image_sha,
   params.id AS params_id,params.sha256 AS params_sha,
   resolved.id AS resolved_id,resolved.sha256 AS resolved_sha
  INTO STRICT producer
  FROM photo.runs r
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND
   w.run_id=r.id AND w.project_id=p_project
  JOIN photo.apply_builds build ON build.tenant_id=r.tenant_id AND
   build.run_id=r.id AND build.graph_version=4
  JOIN photo.snapshot_items item ON item.tenant_id=r.tenant_id AND
   item.snapshot_id=r.input_snapshot_id AND
   item.asset_version_id=e.asset_version_id AND
   item.artifact_id=v.master_artifact_id AND item.sha256=v.content_hash
  JOIN photo.apply_nodes node ON node.tenant_id=r.tenant_id AND
   node.run_id=r.id AND node.phase='portrait_detail' AND
   node.operation='portrait.detail' AND
   node.source_item_id=item.item_id AND
   node.source_asset_id=v.asset_id AND
   node.source_version_id=e.asset_version_id AND
   CASE WHEN jsonb_typeof(node.params->'regions')='array' THEN
    jsonb_array_length(node.params->'regions') BETWEEN 1 AND 128
   ELSE false END
  JOIN photo.tasks detail ON detail.tenant_id=node.tenant_id AND
   detail.id=node.id AND detail.run_id=r.id AND
   detail.operation='portrait.detail' AND detail.operation_version='1' AND
   detail.state='succeeded' AND detail.canonical_result_id IS NOT NULL AND
   detail.created_at<=e.created_at
  JOIN photo.task_results dr ON dr.tenant_id=detail.tenant_id AND
   dr.id=detail.canonical_result_id AND dr.task_id=detail.id AND
   dr.created_at<=e.created_at
  JOIN photo.task_attempts da ON da.tenant_id=dr.tenant_id AND
   da.task_id=detail.id AND da.attempt=dr.attempt AND
   da.fencing_token=dr.fencing_token
  JOIN photo.artifacts report ON report.tenant_id=detail.tenant_id AND
   report.created_by_task_id=detail.id AND
   report.produced_attempt=dr.attempt AND
   report.produced_fence=dr.fencing_token AND
   report.output_role='portrait_detail_report' AND
   report.media_type='application/json' AND
   report.state='ready' AND report.verified_at IS NOT NULL AND
   report.object_version IS NOT NULL AND
   report.object_version NOT IN ('','null') AND
   (report.expires_at IS NULL OR report.expires_at>clock_timestamp()) AND
   dr.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',report.id::text,'sha256',report.sha256))
  JOIN photo.apply_nodes image_node ON
   image_node.tenant_id=node.tenant_id AND
   image_node.run_id=r.id AND
   image_node.id=ANY(node.prerequisites) AND
   image_node.source_item_id=item.item_id AND
   image_node.source_version_id=e.asset_version_id AND
   image_node.phase IN ('local','beauty')
  JOIN photo.tasks image_task ON
   image_task.tenant_id=image_node.tenant_id AND
   image_task.id=image_node.id AND image_task.run_id=r.id AND
   image_task.state='succeeded' AND
   image_task.canonical_result_id IS NOT NULL
  JOIN photo.task_results ir ON ir.tenant_id=image_task.tenant_id AND
   ir.id=image_task.canonical_result_id AND ir.task_id=image_task.id
  JOIN photo.artifacts img ON img.tenant_id=image_task.tenant_id AND
   img.created_by_task_id=image_task.id AND
   img.produced_attempt=ir.attempt AND
   img.produced_fence=ir.fencing_token AND
   img.output_role='working_image' AND
   img.media_type='application/json' AND img.state='ready' AND
   img.verified_at IS NOT NULL AND img.object_version IS NOT NULL AND
   img.object_version NOT IN ('','null') AND
   (img.expires_at IS NULL OR img.expires_at>clock_timestamp()) AND
   ir.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',img.id::text,'sha256',img.sha256))
  JOIN photo.apply_nodes resolved_node ON
   resolved_node.tenant_id=node.tenant_id AND
   resolved_node.run_id=r.id AND
   resolved_node.id=ANY(node.prerequisites) AND
   resolved_node.source_item_id=item.item_id AND
   resolved_node.source_version_id=e.asset_version_id AND
   resolved_node.phase='resolve' AND
   resolved_node.operation='style.resolve'
  JOIN photo.tasks resolved_task ON
   resolved_task.tenant_id=resolved_node.tenant_id AND
   resolved_task.id=resolved_node.id AND
   resolved_task.run_id=r.id AND resolved_task.state='succeeded' AND
   resolved_task.canonical_result_id IS NOT NULL
  JOIN photo.task_results rr ON rr.tenant_id=resolved_task.tenant_id AND
   rr.id=resolved_task.canonical_result_id AND
   rr.task_id=resolved_task.id
  JOIN photo.artifacts resolved ON
   resolved.tenant_id=resolved_task.tenant_id AND
   resolved.created_by_task_id=resolved_task.id AND
   resolved.produced_attempt=rr.attempt AND
   resolved.produced_fence=rr.fencing_token AND
   resolved.output_role='resolved_style' AND
   resolved.media_type='application/json' AND
   resolved.state='ready' AND resolved.verified_at IS NOT NULL AND
   resolved.object_version IS NOT NULL AND
   resolved.object_version NOT IN ('','null') AND
   (resolved.expires_at IS NULL OR resolved.expires_at>clock_timestamp()) AND
   rr.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',resolved.id::text,'sha256',resolved.sha256))
  JOIN photo.artifacts params ON params.tenant_id=detail.tenant_id AND
   params.id=detail.params_artifact_id AND
   params.sha256=node.params_sha256 AND params.state='ready' AND
   params.verified_at IS NOT NULL AND
   params.object_version IS NOT NULL AND
   params.object_version NOT IN ('','null') AND
   (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
  JOIN photo.backend_documents pd ON pd.tenant_id=detail.tenant_id AND
   pd.run_id=r.id AND pd.owner_id=detail.id AND
   pd.purpose='task_params' AND pd.state='ready' AND
   pd.artifact_id=params.id AND pd.sha256=params.sha256 AND
   pd.object_version=params.object_version
  WHERE r.tenant_id=p_tenant AND r.created_by=e.approved_by AND
   NOT r.cancel_requested AND
   detail.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',img.id::text,'sha256',img.sha256)) AND
   detail.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',resolved.id::text,'sha256',resolved.sha256)) AND
   node.params->'image'=jsonb_build_object(
    'artifact_id',img.id,'sha256',img.sha256) AND
   node.params->'resolved'=jsonb_build_object(
    'artifact_id',resolved.id,'sha256',resolved.sha256) AND
   EXISTS(SELECT 1 FROM photo.apply_v4_stages stage WHERE
    stage.tenant_id=r.tenant_id AND stage.run_id=r.id AND
    stage.phase='portrait_detail' AND stage.operation='portrait.detail');
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN;
 END;
 RETURN QUERY SELECT e.asset_version_id,v.master_artifact_id,v.content_hash,
  e.consent_id,e.consent_epoch,producer.run_id,producer.task_id,
  producer.result_id,producer.attempt_id,producer.image_id,
  producer.image_sha,producer.params_id,producer.params_sha,
  producer.resolved_id,producer.resolved_sha;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_portrait_detail_event_producer(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_portrait_detail_event_producer(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_portrait_detail_event_producer(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
