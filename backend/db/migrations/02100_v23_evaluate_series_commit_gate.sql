-- Recheck every series holdout source and its full Stats inventory inside
-- the atomic Evaluate close transaction. Fit may contain training examples;
-- only the Evaluate task's holdout subset is required here.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_series_closure_current(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item record; source record; stat jsonb; node_params jsonb;
 total bigint; found_count bigint:=0; stat_id uuid; stat_sha text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_task IS NULL THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id=p_task AND t.operation='learning.evaluate'
  AND t.operation_version='1' AND t.state='blocked') THEN RETURN false; END IF;
 SELECT count(*) INTO total FROM photo.v2_task_inputs i WHERE
  i.tenant_id=p_tenant AND i.task_id=p_task AND
  i.contract='FrozenSeriesHoldoutEvidence';
 FOR item IN SELECT i.artifact_id,i.sha256,d.owner_id AS event_id
  FROM photo.v2_task_inputs i JOIN photo.backend_documents d
   ON d.tenant_id=i.tenant_id AND d.artifact_id=i.artifact_id AND
   d.sha256=i.sha256 AND d.run_id=p_run AND
   d.purpose='v23_series_holdout' AND d.state='ready'
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.contract='FrozenSeriesHoldoutEvidence'
  ORDER BY i.artifact_id FOR SHARE OF d
 LOOP
  found_count:=found_count+1;
  SELECT * INTO source FROM photo.v23_evaluate_series_authority(
   p_tenant,p_run,p_expansion,p_token,item.event_id,
   item.artifact_id,item.sha256);
  IF NOT FOUND THEN RETURN false; END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.artifact_id=source.source_id AND i.sha256=source.source_sha AND
   i.contract='WorkingImage') OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
    i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.stage_id AND i.sha256=source.stage_sha AND
    i.contract='WorkingImage') OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
    i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.params_id AND i.sha256=source.params_sha AND
    i.contract='SeriesParams') OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
    i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.series_id AND i.sha256=source.series_sha AND
    i.contract='SeriesCorrections') THEN RETURN false; END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_series_nested(
    p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
    item.sha256,'stage_image',source.stage_id,source.stage_sha)) OR
   NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_series_nested(
    p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
    item.sha256,'params',source.params_id,source.params_sha)) OR
   NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_series_nested(
    p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
    item.sha256,'series',source.series_id,source.series_sha)) THEN
   RETURN false; END IF;
  SELECT n.params INTO node_params FROM photo.apply_nodes n WHERE
   n.tenant_id=p_tenant AND n.run_id=source.apply_run_id AND
   n.id=source.producer_task_id AND n.phase='series_balance' AND
   n.operation='series.balance' AND n.params_sha256=source.params_sha
   FOR SHARE;
  IF node_params IS NULL OR jsonb_typeof(node_params->'stats')<>'array' OR
   jsonb_array_length(node_params->'stats') NOT BETWEEN 2 AND 2000 THEN
   RETURN false; END IF;
  FOR stat IN SELECT value FROM jsonb_array_elements(node_params->'stats') LOOP
   IF jsonb_typeof(stat) IS DISTINCT FROM 'object' OR
    (stat-'artifact_id'-'sha256')<>'{}'::jsonb OR
    stat->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false; END IF;
   stat_id:=(stat->>'artifact_id')::uuid;
   stat_sha:=stat->>'sha256';
   IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
    i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=stat_id AND i.sha256=stat_sha AND
    i.contract='SeriesStats') OR
    NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_series_nested(
     p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
     item.sha256,'stats',stat_id,stat_sha)) THEN RETURN false; END IF;
  END LOOP;
 END LOOP;
 RETURN found_count=total;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR
 invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_series_closure_current(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_series_closure_current(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_series_closure_current(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.close_v23_learning_evaluate_guarded(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.v23_evaluate_defect_closure_current(p_tenant,p_run,
  p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_dust_closure_current(p_tenant,p_run,
   p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_detail_closure_current(p_tenant,p_run,
   p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_detail_protection_current(p_tenant,p_run,
   p_expansion,p_token,p_task) OR
  NOT photo.v23_evaluate_series_closure_current(p_tenant,p_run,
   p_expansion,p_token,p_task) THEN RETURN false; END IF;
 RETURN photo.close_v23_learning_evaluate(p_tenant,p_run,
  p_expansion,p_token,p_task);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
