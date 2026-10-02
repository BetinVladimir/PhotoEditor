-- Return only the adopted CandidateProfile produced by the fit task whose
-- result opened the evaluation barrier. This is a late-bound compiler input.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v23_learning_candidate_input(p_tenant uuid,p_run uuid)
RETURNS TABLE(task_id uuid,artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE; t photo.tasks%ROWTYPE;
 z photo.task_results%ROWTYPE; a photo.artifacts%ROWTYPE;
BEGIN
 SELECT * INTO e FROM photo.graph_expansions x
  WHERE x.tenant_id=p_tenant AND x.run_id=p_run
   AND x.kind='v2.learning.evaluate/1' AND x.state='open';
 IF e.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.runs r
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.tenants tenant ON tenant.id=r.tenant_id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='running' AND NOT r.graph_sealed AND NOT r.cancel_requested
   AND w.workflow_type='learning' AND w.renderer_version='v2.2'
   AND tenant.status='active') THEN RETURN; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND id=e.parent_task_id;
 IF t.id IS NULL OR t.operation<>'learning.fit' OR
  t.operation_version<>'1' OR t.node_key<>'learning.fit:0' OR
  t.state<>'succeeded' OR t.canonical_result_id IS NULL OR
  NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
   AND i.consumer='v2-adopt' AND
   i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
  THEN RETURN; END IF;
 SELECT * INTO z FROM photo.task_results result WHERE result.tenant_id=p_tenant
  AND result.task_id=t.id AND result.id=t.canonical_result_id
  AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token;
 IF z.id IS NULL OR jsonb_typeof(z.outputs)<>'array' OR
  jsonb_array_length(z.outputs)<>1 OR
  coalesce(z.outputs->0->>'artifact_id','') !~
   '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  THEN RETURN; END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=(z.outputs->0->>'artifact_id')::uuid;
 IF a.id IS NULL OR a.sha256 IS DISTINCT FROM z.outputs->0->>'sha256'
  OR a.state<>'ready' OR a.output_role<>'document' OR
  a.media_type<>'application/json' OR a.object_version IS NULL OR
  a.verified_at IS NULL OR a.created_by_task_id<>t.id OR
  a.produced_attempt<>t.attempt OR a.produced_fence<>t.fencing_token OR
  (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
  THEN RETURN; END IF;
 RETURN QUERY SELECT t.id,a.id,a.sha256;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v23_learning_candidate_input(uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v23_learning_candidate_input(uuid,uuid)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v23_learning_candidate_input(uuid,uuid)
 TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_v23_learning_evaluate()
RETURNS TABLE(expansion_id uuid,lease_token uuid,lease_until timestamptz,
 tenant_id uuid,run_id uuid,workflow_id uuid,candidate_artifact_id uuid,
 candidate_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE hint photo.graph_expansions%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; candidate record;
 fence uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 SELECT x.* INTO hint FROM photo.graph_expansions x
 JOIN photo.runs rr ON rr.tenant_id=x.tenant_id AND rr.id=x.run_id
 JOIN photo.v2_workflows ww ON ww.tenant_id=x.tenant_id AND ww.run_id=x.run_id
 JOIN photo.tenants tenant ON tenant.id=x.tenant_id
 WHERE x.kind='v2.learning.evaluate/1' AND x.state='open'
  AND x.next_expansion_at<=clock_timestamp()
  AND (x.expansion_lease_until IS NULL OR
       x.expansion_lease_until<=clock_timestamp())
  AND rr.kind='v2' AND rr.status='running' AND NOT rr.graph_sealed
  AND NOT rr.cancel_requested AND ww.workflow_type='learning'
  AND ww.renderer_version='v2.2' AND tenant.status='active'
 ORDER BY x.next_expansion_at,x.created_at,x.id LIMIT 1;
 IF hint.id IS NULL THEN RETURN; END IF;
 SELECT * INTO r FROM photo.runs rr WHERE rr.tenant_id=hint.tenant_id
  AND rr.id=hint.run_id FOR UPDATE SKIP LOCKED;
 IF r.id IS NULL OR r.cancel_requested OR r.status<>'running' OR
  r.graph_sealed THEN RETURN; END IF;
 SELECT * INTO e FROM photo.graph_expansions x WHERE x.tenant_id=hint.tenant_id
  AND x.id=hint.id AND x.run_id=hint.run_id FOR UPDATE SKIP LOCKED;
 IF e.id IS NULL OR e.state<>'open' OR
  e.kind<>'v2.learning.evaluate/1' OR
  (e.expansion_lease_until IS NOT NULL AND
   e.expansion_lease_until>clock_timestamp()) THEN RETURN; END IF;
 SELECT * INTO candidate FROM photo.v23_learning_candidate_input(e.tenant_id,
  e.run_id);
 IF candidate.artifact_id IS NULL THEN RETURN; END IF;
 SELECT * INTO w FROM photo.v2_workflows ww WHERE ww.tenant_id=e.tenant_id
  AND ww.run_id=e.run_id AND ww.workflow_type='learning';
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.graph_expansions x SET expansion_token=fence,
  expansion_lease_until=until_time WHERE x.tenant_id=e.tenant_id AND x.id=e.id;
 RETURN QUERY SELECT e.id,fence,until_time,e.tenant_id,e.run_id,w.id,
  candidate.artifact_id,candidate.sha256;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_v23_learning_evaluate()
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_v23_learning_evaluate()
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_v23_learning_evaluate()
 TO photo_scheduler;
