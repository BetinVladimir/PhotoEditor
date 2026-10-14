-- A reviewed correction may use a SeriesCorrections holdout only when one
-- completed Apply v4 path proves its exact item, color image and aggregate.
-- This is source evidence, not permission to schedule learning.evaluate.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_event_source(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,p_event uuid)
RETURNS TABLE(asset_version_id uuid,consent_id uuid,consent_epoch bigint,
 apply_run_id uuid,item_id uuid,producer_task_id uuid,
 source_image_id uuid,source_image_sha text,
 stage_image_id uuid,stage_image_sha text,
 params_id uuid,params_sha text,series_id uuid,series_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.correction_events%ROWTYPE;
 v photo.asset_versions%ROWTYPE;
 source_row record;
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
  g.project_id=p_project AND g.id=e.consent_id AND g.epoch=e.consent_epoch
  AND g.status='granted' AND g.person_id IS NOT DISTINCT FROM e.person_id
  AND g.purpose=CASE WHEN e.person_id IS NULL THEN
   'learning_photographer' ELSE 'learning_person' END FOR SHARE;
 IF NOT FOUND THEN RETURN; END IF;

 BEGIN
  SELECT r.id AS apply_run_id,item.item_id,balance.id AS producer_task_id,
   source.id AS source_image_id,source.sha256 AS source_image_sha,
   stage.id AS stage_image_id,stage.sha256 AS stage_image_sha,
   params.id AS params_id,params.sha256 AS params_sha,
   series.id AS series_id,series.sha256 AS series_sha
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
  JOIN photo.artifacts source ON source.tenant_id=r.tenant_id AND
   source.id=item.artifact_id AND source.sha256=item.sha256 AND
   source.state='ready' AND source.verified_at IS NOT NULL AND
   source.object_version IS NOT NULL AND source.object_version NOT IN('','null') AND
   (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
  JOIN photo.apply_nodes color_node ON color_node.tenant_id=r.tenant_id AND
   color_node.run_id=r.id AND color_node.phase='color' AND
   color_node.operation='image.color' AND color_node.source_item_id=item.item_id AND
   color_node.source_version_id=e.asset_version_id
  JOIN photo.tasks color ON color.tenant_id=r.tenant_id AND color.id=color_node.id
   AND color.run_id=r.id AND color.operation='image.color' AND
   color.state='succeeded' AND color.canonical_result_id IS NOT NULL
  JOIN photo.task_results color_result ON color_result.tenant_id=r.tenant_id AND
   color_result.id=color.canonical_result_id AND color_result.task_id=color.id
  JOIN photo.artifacts stage ON stage.tenant_id=r.tenant_id AND
   stage.created_by_task_id=color.id AND stage.produced_attempt=color_result.attempt AND
   stage.produced_fence=color_result.fencing_token AND
   stage.output_role='working_image' AND stage.media_type='application/json' AND
   stage.state='ready' AND stage.verified_at IS NOT NULL AND
   stage.object_version IS NOT NULL AND stage.object_version NOT IN('','null') AND
   (stage.expires_at IS NULL OR stage.expires_at>clock_timestamp()) AND
   color_result.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256))
  JOIN photo.apply_nodes measure_node ON measure_node.tenant_id=r.tenant_id AND
   measure_node.run_id=r.id AND measure_node.phase='series_measure' AND
   measure_node.operation='series.measure' AND measure_node.source_item_id=item.item_id AND
   measure_node.source_version_id=e.asset_version_id AND
   measure_node.params->>'item_id'=item.item_id::text AND
   measure_node.params->'image'=jsonb_build_object(
    'artifact_id',stage.id,'sha256',stage.sha256) AND
   color.id=ANY(measure_node.prerequisites)
  JOIN photo.tasks measure ON measure.tenant_id=r.tenant_id AND
   measure.id=measure_node.id AND measure.run_id=r.id AND
   measure.operation='series.measure' AND measure.state='succeeded' AND
   measure.canonical_result_id IS NOT NULL AND
   measure.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256))
  JOIN photo.task_results measure_result ON measure_result.tenant_id=r.tenant_id AND
   measure_result.id=measure.canonical_result_id AND measure_result.task_id=measure.id
  JOIN photo.artifacts stats ON stats.tenant_id=r.tenant_id AND
   stats.created_by_task_id=measure.id AND
   stats.produced_attempt=measure_result.attempt AND
   stats.produced_fence=measure_result.fencing_token AND
   stats.output_role='series_stats' AND stats.media_type='application/json' AND
   stats.state='ready' AND stats.verified_at IS NOT NULL AND
   stats.object_version IS NOT NULL AND stats.object_version NOT IN('','null') AND
   (stats.expires_at IS NULL OR stats.expires_at>clock_timestamp()) AND
   measure_result.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stats.id::text,'sha256',stats.sha256))
  JOIN photo.apply_nodes balance_node ON balance_node.tenant_id=r.tenant_id AND
   balance_node.run_id=r.id AND balance_node.phase='series_balance' AND
   balance_node.operation='series.balance' AND
   measure.id=ANY(balance_node.prerequisites) AND
   balance_node.params->'stats' @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stats.id,'sha256',stats.sha256))
  JOIN photo.tasks balance ON balance.tenant_id=r.tenant_id AND
   balance.id=balance_node.id AND balance.run_id=r.id AND
   balance.operation='series.balance' AND balance.operation_version='1' AND
   balance.state='succeeded' AND balance.canonical_result_id IS NOT NULL AND
   balance.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stats.id::text,'sha256',stats.sha256))
  JOIN photo.task_results balance_result ON balance_result.tenant_id=r.tenant_id AND
   balance_result.id=balance.canonical_result_id AND balance_result.task_id=balance.id
  JOIN photo.artifacts params ON params.tenant_id=r.tenant_id AND
   params.id=balance.params_artifact_id AND params.sha256=balance_node.params_sha256 AND
   params.media_type='application/json' AND params.state='ready' AND
   params.verified_at IS NOT NULL AND params.object_version IS NOT NULL AND
   params.object_version NOT IN('','null') AND
   (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
  JOIN photo.backend_documents params_doc ON params_doc.tenant_id=r.tenant_id AND
   params_doc.run_id=r.id AND params_doc.owner_id=balance.id AND
   params_doc.purpose='task_params' AND params_doc.state='ready' AND
   params_doc.artifact_id=params.id AND params_doc.sha256=params.sha256 AND
   params_doc.object_version=params.object_version
  JOIN photo.artifacts series ON series.tenant_id=r.tenant_id AND
   series.created_by_task_id=balance.id AND
   series.produced_attempt=balance_result.attempt AND
   series.produced_fence=balance_result.fencing_token AND
   series.output_role='series_corrections' AND series.media_type='application/json' AND
   series.state='ready' AND series.verified_at IS NOT NULL AND
   series.object_version IS NOT NULL AND series.object_version NOT IN('','null') AND
   (series.expires_at IS NULL OR series.expires_at>clock_timestamp()) AND
   balance_result.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',series.id::text,'sha256',series.sha256))
  JOIN photo.apply_nodes local_node ON local_node.tenant_id=r.tenant_id AND
   local_node.run_id=r.id AND local_node.phase='local' AND
   local_node.operation='image.local' AND local_node.source_item_id=item.item_id AND
   local_node.source_version_id=e.asset_version_id AND
   local_node.params->>'item_id'=item.item_id::text AND
   local_node.params->'image'=jsonb_build_object(
    'artifact_id',stage.id,'sha256',stage.sha256) AND
   local_node.params->'series'=jsonb_build_object(
    'artifact_id',series.id,'sha256',series.sha256) AND
   balance.id=ANY(local_node.prerequisites)
  JOIN photo.tasks local_task ON local_task.tenant_id=r.tenant_id AND
   local_task.id=local_node.id AND local_task.run_id=r.id AND
   local_task.operation='image.local' AND local_task.state='succeeded' AND
   local_task.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',series.id::text,'sha256',series.sha256))
  WHERE r.tenant_id=p_tenant AND r.kind='apply' AND r.status='completed' AND
   r.graph_sealed AND NOT r.cancel_requested;
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN;
 END;
 RETURN QUERY SELECT e.asset_version_id,e.consent_id,e.consent_epoch,
  source_row.apply_run_id,source_row.item_id,source_row.producer_task_id,
  source_row.source_image_id,source_row.source_image_sha,
  source_row.stage_image_id,source_row.stage_image_sha,
  source_row.params_id,source_row.params_sha,
  source_row.series_id,source_row.series_sha;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_event_source(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_event_source(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_series_event_source(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
