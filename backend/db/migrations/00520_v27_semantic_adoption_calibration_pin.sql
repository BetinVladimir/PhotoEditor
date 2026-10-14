-- +goose Up
-- Attempt/result-scoped locked qualification predicate for semantic adoption.
-- Bind the registered calibration to the frozen qualification identity.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v27_semantic_adoption_qualification_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE owner_run uuid; t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; pin jsonb;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_result IS NULL THEN RETURN false;END IF;
 SELECT run_id INTO owner_run FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF owner_run IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=owner_run FOR SHARE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=owner_run FOR SHARE;
 IF t.id IS NULL OR t.operation NOT IN('semantic.transfer','v2.semantic.assemble') OR t.operation_version<>'1'
 OR t.state<>'succeeded' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.canonical_result_id IS DISTINCT FROM p_result OR r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
 OR NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=p_task AND id=p_result AND attempt=p_attempt AND fencing_token=p_fence)
 OR NOT photo.v2_input_closure_ready(p_task) OR NOT photo.v27_semantic_frozen_qualification_ready(p_tenant,owner_run) THEN RETURN false;END IF;
 pin:=r.config_snapshot->'v27_semantic'->'qualification';
 IF (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='Calibration')<>1
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs WHERE tenant_id=p_tenant AND task_id=p_task AND contract='Calibration'
 AND artifact_id::text=pin->'calibration'->>'artifact_id' AND sha256=pin->'calibration'->>'sha256') THEN RETURN false;END IF;
 PERFORM p.id FROM photo.projects p JOIN photo.v2_workflows w ON w.tenant_id=p.tenant_id AND w.project_id=p.id
 WHERE w.tenant_id=p_tenant AND w.run_id=owner_run FOR SHARE OF p;
 PERFORM id FROM photo.v2_qualification_policies WHERE id=(pin->>'policy_id')::uuid FOR SHARE;
 PERFORM id FROM photo.model_qualifications WHERE tenant_id=p_tenant AND id=(pin->>'qualification_id')::uuid FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN((pin->'report'->>'artifact_id')::uuid,(pin->'calibration'->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 RETURN photo.v27_semantic_frozen_qualification_ready(p_tenant,owner_run) AND photo.v2_input_closure_ready(p_task);
END $$;
ALTER FUNCTION photo.v27_semantic_adoption_qualification_ready(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_adoption_qualification_ready(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_adoption_qualification_ready(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
