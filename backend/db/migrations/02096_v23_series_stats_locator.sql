-- Locate every stats body from the same sealed Apply balance, including
-- items other than the selected learning event. Call on both sides of IO.
-- The V23 series freeze gate remains in force.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_series_stat_artifact(
 p_tenant uuid,p_project uuid,p_actor uuid,p_auth_session uuid,
 p_event uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(item_id uuid,image_id uuid,image_sha text,bucket text,
 object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_row record; located record;
BEGIN
 IF p_tenant IS NULL OR p_project IS NULL OR p_actor IS NULL OR
  p_auth_session IS NULL OR p_event IS NULL OR p_artifact IS NULL OR
  p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' THEN RETURN; END IF;
 SELECT * INTO source_row FROM photo.v23_series_event_source(
  p_tenant,p_project,p_actor,p_auth_session,p_event);
 IF NOT FOUND THEN RETURN; END IF;
 BEGIN
  SELECT mn.source_item_id AS item_id,stage.id AS image_id,
   stage.sha256 AS image_sha,stat.bucket,stat.object_key,
   stat.object_version,stat.sha256,stat.bytes,stat.media_type
   INTO STRICT located
  FROM photo.apply_nodes bn
  JOIN photo.tasks balance ON balance.tenant_id=bn.tenant_id AND
   balance.id=bn.id AND balance.run_id=bn.run_id AND
   balance.operation='series.balance' AND balance.state='succeeded' AND
   balance.params_artifact_id=source_row.params_id AND
   balance.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',p_artifact::text,'sha256',p_sha))
  JOIN photo.apply_nodes mn ON mn.tenant_id=bn.tenant_id AND
   mn.run_id=bn.run_id AND mn.id=ANY(bn.prerequisites) AND
   mn.phase='series_measure' AND mn.operation='series.measure' AND
   mn.params->>'item_id'=mn.source_item_id::text
  JOIN photo.snapshot_items si ON si.tenant_id=bn.tenant_id AND
   si.snapshot_id=(SELECT input_snapshot_id FROM photo.runs r WHERE
    r.tenant_id=bn.tenant_id AND r.id=bn.run_id) AND
   si.item_id=mn.source_item_id AND
   si.asset_version_id=mn.source_version_id
  JOIN photo.tasks measure ON measure.tenant_id=bn.tenant_id AND
   measure.id=mn.id AND measure.run_id=mn.run_id AND
   measure.operation='series.measure' AND measure.state='succeeded' AND
   measure.canonical_result_id IS NOT NULL
  JOIN photo.task_results mr ON mr.tenant_id=bn.tenant_id AND
   mr.id=measure.canonical_result_id AND mr.task_id=measure.id AND
   mr.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',p_artifact::text,'sha256',p_sha))
  JOIN photo.artifacts stat ON stat.tenant_id=bn.tenant_id AND
   stat.id=p_artifact AND stat.sha256=p_sha AND
   stat.created_by_task_id=measure.id AND
   stat.produced_attempt=mr.attempt AND
   stat.produced_fence=mr.fencing_token AND
   stat.output_role='series_stats' AND
   stat.media_type='application/json' AND stat.state='ready' AND
   stat.verified_at IS NOT NULL AND stat.object_version IS NOT NULL AND
   stat.object_version NOT IN ('','null') AND
   stat.bytes BETWEEN 2 AND 1048576 AND
   (stat.expires_at IS NULL OR stat.expires_at>clock_timestamp())
  JOIN photo.apply_nodes cn ON cn.tenant_id=bn.tenant_id AND
   cn.run_id=bn.run_id AND cn.phase='color' AND
   cn.operation='image.color' AND cn.source_item_id=mn.source_item_id AND
   cn.source_version_id=mn.source_version_id AND
   cn.id=ANY(mn.prerequisites)
  JOIN photo.tasks color ON color.tenant_id=bn.tenant_id AND
   color.id=cn.id AND color.run_id=cn.run_id AND
   color.operation='image.color' AND color.state='succeeded' AND
   color.canonical_result_id IS NOT NULL
  JOIN photo.task_results cr ON cr.tenant_id=bn.tenant_id AND
   cr.id=color.canonical_result_id AND cr.task_id=color.id
  JOIN photo.artifacts stage ON stage.tenant_id=bn.tenant_id AND
   stage.id::text=mn.params->'image'->>'artifact_id' AND
   stage.sha256=mn.params->'image'->>'sha256' AND
   stage.created_by_task_id=color.id AND
   stage.produced_attempt=cr.attempt AND
   stage.produced_fence=cr.fencing_token AND
   stage.output_role='working_image' AND
   stage.media_type='application/json' AND stage.state='ready' AND
   stage.verified_at IS NOT NULL AND stage.object_version IS NOT NULL AND
   stage.object_version NOT IN ('','null') AND
   (stage.expires_at IS NULL OR stage.expires_at>clock_timestamp()) AND
   cr.outputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256)) AND
   measure.inputs @> jsonb_build_array(jsonb_build_object(
    'artifact_id',stage.id::text,'sha256',stage.sha256))
  WHERE bn.tenant_id=p_tenant AND bn.run_id=source_row.apply_run_id AND
   bn.id=source_row.producer_task_id AND bn.phase='series_balance' AND
   bn.operation='series.balance' AND
   bn.params->'stats' @> jsonb_build_array(jsonb_build_object(
    'artifact_id',p_artifact,'sha256',p_sha));
 EXCEPTION WHEN NO_DATA_FOUND OR TOO_MANY_ROWS THEN RETURN;
 END;
 RETURN QUERY SELECT located.item_id,located.image_id,located.image_sha,
  located.bucket,located.object_key,located.object_version,
  located.sha256,located.bytes,located.media_type;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_series_stat_artifact(
 uuid,uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_series_stat_artifact(
 uuid,uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_series_stat_artifact(
 uuid,uuid,uuid,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
