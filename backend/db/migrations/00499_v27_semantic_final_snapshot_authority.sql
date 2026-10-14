-- +goose Up
-- Repeat live snapshot authority after retaining all authority/catalog rows.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE assembly photo.tasks%ROWTYPE; owner_run uuid; run_row photo.runs%ROWTYPE; workflow photo.v2_workflows%ROWTYPE; auth_session uuid;
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
   SELECT * INTO run_row FROM photo.runs WHERE tenant_id=p_tenant AND id=owner_run;
   SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=owner_run;
   BEGIN
    auth_session:=(run_row.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid;
   EXCEPTION WHEN invalid_text_representation THEN RETURN false;END;
   PERFORM id FROM photo.users WHERE id=run_row.created_by FOR SHARE;
   PERFORM id FROM photo.sessions WHERE id=auth_session AND user_id=run_row.created_by FOR SHARE;
   PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=run_row.created_by FOR SHARE;
   PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=workflow.project_id FOR SHARE;
   PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=workflow.project_id AND user_id=run_row.created_by FOR SHARE;
   PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=run_row.input_snapshot_id FOR SHARE;
   PERFORM v.id FROM photo.asset_versions v JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
    WHERE i.tenant_id=p_tenant AND i.snapshot_id=run_row.input_snapshot_id ORDER BY v.id FOR SHARE OF v;
   PERFORM asset.id FROM photo.assets asset JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id
    JOIN photo.snapshot_items i ON i.tenant_id=v.tenant_id AND i.asset_version_id=v.id
    WHERE i.tenant_id=p_tenant AND i.snapshot_id=run_row.input_snapshot_id ORDER BY asset.id FOR SHARE OF asset;
   PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND
    (EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
      AND i.contract='TransferCandidate' AND i.artifact_id=a.id)
     OR EXISTS(SELECT 1 FROM photo.tasks producer JOIN photo.v2_task_inputs i ON i.tenant_id=producer.tenant_id
      AND i.task_id=p_task AND i.contract='TransferCandidate'
      JOIN photo.artifacts candidate ON candidate.tenant_id=i.tenant_id AND candidate.id=i.artifact_id
      AND candidate.created_by_task_id=producer.id
      WHERE producer.tenant_id=p_tenant AND producer.run_id=owner_run AND producer.params_artifact_id=a.id)
     OR EXISTS(SELECT 1 FROM photo.v2_task_inputs declared WHERE declared.tenant_id=p_tenant AND declared.task_id=p_task
      AND declared.contract='InputSnapshot' AND declared.artifact_id=a.id)
     OR EXISTS(SELECT 1 FROM photo.snapshot_items item JOIN photo.asset_versions v ON v.tenant_id=item.tenant_id AND v.id=item.asset_version_id
      JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
      WHERE item.tenant_id=p_tenant AND item.snapshot_id=run_row.input_snapshot_id
      AND a.id IN(item.artifact_id,asset.original_artifact_id)))
    ORDER BY a.id FOR SHARE OF a;
   IF NOT photo.v27_semantic_assembly_inputs_ready(p_tenant,p_task,p_attempt,p_fence,p_result)
    OR NOT EXISTS(SELECT 1 FROM photo.v27_semantic_assembly_snapshot(p_tenant,p_task,p_attempt,p_fence,p_result)) THEN RETURN false;END IF;
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
