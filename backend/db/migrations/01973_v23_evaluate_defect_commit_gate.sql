-- Recheck live reviewed evidence inside the transaction that closes Evaluate.
-- The authority reader takes SHARE locks on tenant, event, version and consent.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_evaluate_defect_closure_current(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item record; source record; count_inputs bigint;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_task IS NULL THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.id=p_task AND t.operation='learning.evaluate'
  AND t.operation_version='1' AND t.state='blocked') THEN RETURN false; END IF;
 -- A caller must neither omit Fit-declared reviewed evidence nor inject a
 -- different evidence root. The Fit parent is fixed by this leased expansion.
 IF EXISTS(SELECT 1 FROM photo.graph_expansions e
  JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id AND
   fit.run_id=e.run_id AND fit.id=e.parent_task_id
  JOIN photo.v2_task_inputs fi ON fi.tenant_id=fit.tenant_id AND
   fi.task_id=fit.id AND fi.contract='FrozenDefectHoldoutEvidence'
  WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND
   e.id=p_expansion AND e.expansion_token=p_token AND
   fit.operation='learning.fit' AND fit.operation_version='1' AND
   fit.state='succeeded' AND NOT EXISTS(
    SELECT 1 FROM photo.v2_task_inputs ei WHERE ei.tenant_id=p_tenant
     AND ei.task_id=p_task AND ei.contract=fi.contract AND
     ei.artifact_id=fi.artifact_id AND ei.sha256=fi.sha256))
  OR EXISTS(SELECT 1 FROM photo.v2_task_inputs ei
   WHERE ei.tenant_id=p_tenant AND ei.task_id=p_task AND
    ei.contract='FrozenDefectHoldoutEvidence' AND NOT EXISTS(
     SELECT 1 FROM photo.graph_expansions e
     JOIN photo.tasks fit ON fit.tenant_id=e.tenant_id AND
      fit.run_id=e.run_id AND fit.id=e.parent_task_id
     JOIN photo.v2_task_inputs fi ON fi.tenant_id=fit.tenant_id AND
      fi.task_id=fit.id AND fi.contract=ei.contract AND
      fi.artifact_id=ei.artifact_id AND fi.sha256=ei.sha256
     WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND
      e.id=p_expansion AND e.expansion_token=p_token AND
      fit.operation='learning.fit' AND fit.operation_version='1' AND
      fit.state='succeeded')) THEN RETURN false; END IF;
 SELECT count(*) INTO count_inputs FROM photo.v2_task_inputs i
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.contract='FrozenDefectHoldoutEvidence';
 FOR item IN SELECT i.artifact_id,i.sha256,d.owner_id AS event_id
  FROM photo.v2_task_inputs i JOIN photo.backend_documents d
   ON d.tenant_id=i.tenant_id AND d.artifact_id=i.artifact_id AND
   d.sha256=i.sha256 AND d.run_id=p_run AND
   d.purpose='v23_defect_holdout' AND d.state='ready'
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   i.contract='FrozenDefectHoldoutEvidence'
  ORDER BY i.artifact_id FOR SHARE OF d
 LOOP
  SELECT * INTO source FROM photo.v23_evaluate_reviewed_defect_authority(
   p_tenant,p_run,p_expansion,p_token,item.event_id,
   item.artifact_id,item.sha256);
  IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.params_id AND i.sha256=source.params_sha AND
    i.contract='Params') OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.proposal_id AND i.sha256=source.proposal_sha AND
    i.contract='DefectCandidates') OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.labels_id AND i.sha256=source.labels_sha AND
    i.contract='DefectLabelMap') OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.selection_id AND i.sha256=source.selection_sha AND
    i.contract='DefectReview') OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i
   WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
    i.artifact_id=source.resolved_id AND i.sha256=source.resolved_sha AND
    i.contract='ResolvedStyle') THEN RETURN false; END IF;
  count_inputs:=count_inputs-1;
 END LOOP;
 RETURN count_inputs=0;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_evaluate_defect_closure_current(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_evaluate_defect_closure_current(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_evaluate_defect_closure_current(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- The scheduler can only close through this wrapper; otherwise a direct
-- invocation of the earlier close function could skip the live review gate.
-- +goose StatementBegin
CREATE FUNCTION photo.close_v23_learning_evaluate_guarded(
 p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.v23_evaluate_defect_closure_current(p_tenant,p_run,
  p_expansion,p_token,p_task) THEN RETURN false; END IF;
 RETURN photo.close_v23_learning_evaluate(p_tenant,p_run,
  p_expansion,p_token,p_task);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION photo.close_v23_learning_evaluate(
 uuid,uuid,uuid,uuid,uuid) FROM photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.close_v23_learning_evaluate_guarded(
 uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
