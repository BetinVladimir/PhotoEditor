-- +goose Up
-- Keep producer generations stable through the final assembly adoption write.
-- +goose StatementBegin
CREATE FUNCTION photo.v27_semantic_assembly_inputs_ready(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result))
 AND (SELECT count(*) BETWEEN 1 AND 5000 AND coalesce(bool_and(
  EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_producer(p_tenant,p_task,p_attempt,p_fence,p_result,i.artifact_id,i.sha256))),false)
  FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='TransferCandidate');
$$;
ALTER FUNCTION photo.v27_semantic_assembly_inputs_ready(uuid,uuid,integer,bigint,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v27_semantic_assembly_inputs_ready(uuid,uuid,integer,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v27_semantic_assembly_inputs_ready(uuid,uuid,integer,bigint,uuid) TO photo_scheduler;

ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_semantic_producers;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_semantic_producers(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE assembly photo.tasks%ROWTYPE; owner_run uuid;
BEGIN
 SELECT * INTO assembly FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF assembly.operation='v2.semantic.assemble' THEN
  PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT id INTO owner_run FROM photo.runs WHERE tenant_id=p_tenant AND id=assembly.run_id FOR UPDATE;
  IF owner_run IS NULL THEN RETURN false;END IF;
  -- All task locks use one deterministic order, including the assembly itself.
  PERFORM t.id FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=owner_run AND
   (t.id=p_task OR t.id IN(SELECT a.created_by_task_id FROM photo.v2_task_inputs i
    JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
    WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='TransferCandidate'))
   ORDER BY t.id FOR UPDATE OF t;
  SELECT * INTO assembly FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
  IF assembly.operation<>'v2.semantic.assemble' OR assembly.operation_version<>'1'
   OR assembly.attempt IS DISTINCT FROM p_attempt OR assembly.fencing_token IS DISTINCT FROM p_fence
   OR assembly.canonical_result_id IS DISTINCT FROM p_result THEN RETURN false;END IF;
  -- An accepted replay delegates to the existing digest/conflict semantics.
  IF NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
    AND i.event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN
   PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
    (EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
      AND i.contract='TransferCandidate' AND i.artifact_id=a.id)
     OR EXISTS(SELECT 1 FROM photo.tasks producer JOIN photo.v2_task_inputs i ON i.tenant_id=producer.tenant_id
      AND i.task_id=p_task AND i.contract='TransferCandidate'
      JOIN photo.artifacts candidate ON candidate.tenant_id=i.tenant_id AND candidate.id=i.artifact_id
      AND candidate.created_by_task_id=producer.id
      WHERE producer.tenant_id=p_tenant AND producer.run_id=owner_run AND producer.params_artifact_id=a.id))
    ORDER BY a.id FOR SHARE OF a;
   IF NOT photo.v27_semantic_assembly_inputs_ready(p_tenant,p_task,p_attempt,p_fence,p_result) THEN RETURN false;END IF;
  END IF;
 END IF;
 RETURN photo.adopt_v2_plan_pre_semantic_producers(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
