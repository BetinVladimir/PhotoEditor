-- Correct the sealed evaluation barrier to require FrozenRecipe inputs.
-- The former EditingRecipeV3 label disagreed with the immutable V23 contract.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.close_v23_learning_evaluate(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 t photo.tasks%ROWTYPE; d photo.backend_documents%ROWTYPE;
 candidate record; manifest_id uuid; manifest_sha text;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR
  p_token IS NULL OR p_task IS NULL THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs x WHERE x.tenant_id=p_tenant
  AND x.id=p_run FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.id=p_expansion FOR UPDATE;
 SELECT * INTO t FROM photo.tasks x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR
  r.graph_sealed OR r.cancel_requested OR e.id IS NULL OR
  e.kind<>'v2.learning.evaluate/1' OR e.state<>'open' OR
  e.expansion_token IS DISTINCT FROM p_token OR
  e.expansion_lease_until<=clock_timestamp() OR
  e.parent_task_id IS NULL OR t.id IS NULL OR
  t.node_key<>'learning.evaluate:0' OR
  t.operation<>'learning.evaluate' OR t.operation_version<>'1' OR
  t.resource_class<>'gpu' OR t.state<>'blocked' OR t.attempt<>0 OR
  t.cancel_requested OR t.execution_profile->>'backend'<>'cuda' OR
  t.execution_profile->>'renderer_version'<>'v2.2' OR
  NOT photo.v2_input_closure_ready(p_task) OR
  NOT EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant
   AND tenant.status='active') OR
  NOT EXISTS(SELECT 1 FROM photo.quota_reservations q
   WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
    AND q.state='reserved' AND q.expires_at>clock_timestamp()) OR
  NOT EXISTS(SELECT 1 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant
   AND w.run_id=p_run AND w.workflow_type='learning' AND
    w.renderer_version='v2.2' AND w.input_snapshot_id=r.input_snapshot_id) OR
  NOT EXISTS(SELECT 1 FROM photo.operation_registry op
   WHERE op.operation='learning.evaluate' AND op.version='1' AND
    op.resource_class='gpu' AND op.v2_enabled AND op.backends ? 'cuda')
  THEN RETURN false; END IF;
 SELECT * INTO candidate FROM photo.v23_learning_candidate_input(p_tenant,p_run);
 IF candidate.artifact_id IS NULL OR candidate.task_id<>e.parent_task_id
  THEN RETURN false; END IF;
 SELECT * INTO d FROM photo.backend_documents x WHERE x.tenant_id=p_tenant
  AND x.run_id=p_run AND x.owner_id=p_expansion AND
   x.purpose='v23_evaluate_params' AND x.state='ready' FOR SHARE;
 IF d.id IS NULL OR d.artifact_id<>t.params_artifact_id OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=d.artifact_id AND
    i.sha256=d.sha256 AND i.contract='Params') OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=candidate.artifact_id AND
    i.sha256=candidate.sha256 AND i.contract='CandidateProfile') OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='Params')<>1 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='CandidateProfile')<>2 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='Calibration')<>1 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='WorkingImage')<10 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='NpyArray')<10 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='MaskArray')<1 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='StyleV2')<1 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='FrozenRecipe')<2 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='MaskRef')<1 OR
  (SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.contract='FaceObservations')<1
  THEN RETURN false; END IF;
 SELECT i.artifact_id INTO manifest_id FROM photo.snapshot_items i
  WHERE i.tenant_id=p_tenant AND i.snapshot_id=r.input_snapshot_id
   AND i.ordinal=0;
 manifest_sha:=r.config_snapshot->'v23_training'->>'manifest_sha';
 IF manifest_id IS NULL OR manifest_sha IS NULL OR
  NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant
   AND i.task_id=p_task AND i.artifact_id=manifest_id AND
    i.sha256=manifest_sha AND i.contract='TrainingManifest') OR
  NOT EXISTS(SELECT 1 FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
   AND edge.run_id=p_run AND edge.prerequisite_id=e.parent_task_id AND
    edge.dependent_id=p_task AND edge.condition='success') OR
  (SELECT count(*) FROM photo.task_edges edge WHERE edge.tenant_id=p_tenant
   AND edge.run_id=p_run AND edge.dependent_id=p_task)<>1 OR
  EXISTS(SELECT 1 FROM photo.graph_expansions other WHERE other.tenant_id=p_tenant
   AND other.run_id=p_run AND other.id<>p_expansion AND other.state<>'closed')
  THEN RETURN false; END IF;
 UPDATE photo.graph_expansions SET state='closed',cursor='1',
  expansion_token=NULL,expansion_lease_until=NULL
  WHERE tenant_id=p_tenant AND id=p_expansion AND state='open'
   AND expansion_token=p_token;
 IF NOT FOUND THEN RAISE EXCEPTION 'V23_EVALUATE_EXPANSION_RACE' USING ERRCODE='40001'; END IF;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1
  WHERE tenant_id=p_tenant AND id=p_run AND status='running'
   AND NOT cancel_requested AND NOT graph_sealed AND revision<9007199254740991;
 IF NOT FOUND THEN RAISE EXCEPTION 'V23_EVALUATE_SEAL_RACE' USING ERRCODE='40001'; END IF;
 IF NOT photo.activate_bound_task(p_task,t.input_digest,t.params_artifact_id) THEN
  RAISE EXCEPTION 'V23_EVALUATE_ACTIVATION_FAILED' USING ERRCODE='23514';
 END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.close_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.close_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.close_v23_learning_evaluate(uuid,uuid,uuid,uuid,uuid)
 TO photo_scheduler;
