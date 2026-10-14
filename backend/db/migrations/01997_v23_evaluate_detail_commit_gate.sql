-- Recheck the selected Apply portrait.detail producer and every nested input
-- inside the atomic Evaluate close transaction. Fit may contain train examples;
-- only the admitted Evaluate holdout subset is required here.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_detail_closure_current(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item record; source record; mask record; node_params jsonb;
 total bigint; found_count bigint;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_task IS NULL THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id=p_task AND t.operation='learning.evaluate'
  AND t.operation_version='1' AND t.state='blocked') THEN RETURN false; END IF;
 SELECT count(*) INTO total FROM photo.v2_task_inputs i WHERE
  i.tenant_id=p_tenant AND i.task_id=p_task AND
  i.contract='FrozenPortraitDetailHoldoutEvidence';
 found_count:=0;
 FOR item IN SELECT i.artifact_id,i.sha256,d.owner_id AS event_id
  FROM photo.v2_task_inputs i JOIN photo.backend_documents d
   ON d.tenant_id=i.tenant_id AND d.artifact_id=i.artifact_id AND
   d.sha256=i.sha256 AND d.run_id=p_run AND
   d.purpose='v23_detail_holdout' AND d.state='ready'
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.contract='FrozenPortraitDetailHoldoutEvidence'
  ORDER BY i.artifact_id FOR SHARE OF d
 LOOP
  found_count:=found_count+1;
  IF NOT EXISTS(SELECT 1 FROM photo.graph_expansions e
   JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id AND
    fit.run_id=e.run_id AND fit.id=e.parent_task_id AND
    fit.operation='learning.fit' AND fit.operation_version='1' AND
    fit.state='succeeded' AND fit.canonical_result_id IS NOT NULL
   JOIN photo.v2_task_inputs fi ON fi.tenant_id=fit.tenant_id AND
    fi.task_id=fit.id AND fi.contract='FrozenPortraitDetailHoldoutEvidence' AND
    fi.artifact_id=item.artifact_id AND fi.sha256=item.sha256
   WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND
    e.id=p_expansion AND e.expansion_token=p_token AND
    e.kind='v2.learning.evaluate/1' AND e.state='open' AND
    e.expansion_lease_until>clock_timestamp()) THEN RETURN false; END IF;
  SELECT * INTO source FROM photo.v23_evaluate_detail_authority(
   p_tenant,p_run,p_expansion,p_token,item.event_id,
   item.artifact_id,item.sha256);
  IF NOT FOUND THEN RETURN false; END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.artifact_id=source.source_id AND i.sha256=source.source_sha AND
   i.contract='WorkingImage') OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.artifact_id=source.image_id AND i.sha256=source.image_sha AND
   i.contract='WorkingImage') OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.artifact_id=source.params_id AND i.sha256=source.params_sha AND
   i.contract='PortraitParams') OR
   NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
   i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.artifact_id=source.resolved_id AND i.sha256=source.resolved_sha AND
   i.contract='ResolvedStyle') THEN RETURN false; END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_detail_nested(
    p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
    item.sha256,'image',source.image_id,source.image_sha)) OR
   NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_detail_nested(
    p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
    item.sha256,'params',source.params_id,source.params_sha)) OR
   NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_detail_nested(
    p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
    item.sha256,'resolved',source.resolved_id,source.resolved_sha)) THEN
   RETURN false; END IF;
  SELECT n.params INTO node_params FROM photo.apply_nodes n WHERE
   n.tenant_id=p_tenant AND n.run_id=source.producer_run_id AND
   n.id=source.producer_task_id AND n.phase='portrait_detail' AND
   n.operation='portrait.detail' AND n.params_sha256=source.params_sha
   FOR SHARE;
  IF node_params IS NULL THEN RETURN false; END IF;
  FOR mask IN SELECT DISTINCT x.ref->>'artifact_id' AS artifact_id,
    x.ref->>'sha256' AS sha256 FROM (
    SELECT region.value->'mask'->'artifact' AS ref
    FROM jsonb_array_elements(CASE WHEN jsonb_typeof(node_params->'regions')='array'
      THEN node_params->'regions' ELSE '[]'::jsonb END) region
    UNION ALL
    SELECT guard.value->'artifact' AS ref
    FROM jsonb_array_elements(CASE WHEN jsonb_typeof(node_params->'protected')='array'
      THEN node_params->'protected' ELSE '[]'::jsonb END) guard
   ) x
  LOOP
   IF mask.artifact_id IS NULL OR mask.sha256 IS NULL OR
    mask.sha256 !~ '^[0-9a-f]{64}$' OR
    NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE
     i.tenant_id=p_tenant AND i.task_id=p_task AND
     i.artifact_id=mask.artifact_id::uuid AND i.sha256=mask.sha256 AND
     i.contract='NpyArray') OR
    NOT EXISTS(SELECT 1 FROM photo.v23_evaluate_detail_nested(
     p_tenant,p_run,p_expansion,p_token,item.event_id,item.artifact_id,
     item.sha256,'mask',mask.artifact_id::uuid,mask.sha256)) THEN
    RETURN false; END IF;
  END LOOP;
 END LOOP;
 RETURN found_count=total;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_detail_closure_current(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_detail_closure_current(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_detail_closure_current(
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
