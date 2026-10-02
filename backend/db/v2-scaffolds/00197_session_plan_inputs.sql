-- Trusted compiler input: only canonical, adopted descriptor roots are
-- returned, in frozen shard order. No future output reference is fabricated.
-- +goose StatementBegin
CREATE FUNCTION photo.v22_session_plan_inputs(p_tenant uuid,p_run uuid)
RETURNS TABLE(task_id uuid,artifact_id uuid,sha256 text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE v_count integer; v_ready integer;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM photo.runs r JOIN photo.v2_workflows w
  ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.tenants tenant ON tenant.id=r.tenant_id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2'
   AND r.status='running' AND NOT r.cancel_requested AND NOT r.graph_sealed
   AND w.workflow_type='session' AND w.renderer_version='v2.2'
   AND tenant.status='active' AND EXISTS(SELECT 1 FROM photo.graph_expansions e
    WHERE e.tenant_id=p_tenant AND e.run_id=p_run
     AND e.kind='v2.session.plan/1' AND e.state='open')) THEN RETURN; END IF;
 SELECT count(*) INTO v_count FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.operation='session.describe';
 IF v_count NOT BETWEEN 1 AND 40 OR EXISTS(SELECT 1 FROM photo.tasks t
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='session.describe'
   AND t.node_key !~ '^session[.]describe:(0|[1-9]|[1-3][0-9])$') THEN RETURN; END IF;
 SELECT count(*) INTO v_ready FROM photo.tasks t
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id
   AND z.task_id=t.id AND z.id=t.canonical_result_id
   AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
  JOIN photo.artifacts a ON a.tenant_id=t.tenant_id
   AND a.id=(z.outputs->0->>'artifact_id')::uuid
   AND a.sha256=z.outputs->0->>'sha256'
   AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.object_version IS NOT NULL
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='session.describe'
   AND t.state='succeeded' AND jsonb_typeof(z.outputs)='array'
   AND jsonb_array_length(z.outputs)=1 AND a.output_role='document'
   AND a.created_by_task_id=t.id AND a.produced_attempt=t.attempt
   AND a.produced_fence=t.fencing_token
   AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant
    AND i.consumer='v2-adopt'
    AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
   AND (split_part(t.node_key,':',2))::integer BETWEEN 0 AND v_count-1;
 IF v_ready<>v_count OR EXISTS(SELECT 1 FROM photo.tasks t
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='session.describe'
  GROUP BY split_part(t.node_key,':',2) HAVING count(*)<>1) THEN RETURN; END IF;
 RETURN QUERY SELECT t.id,a.id,a.sha256 FROM photo.tasks t
  JOIN photo.task_results z ON z.tenant_id=t.tenant_id
   AND z.task_id=t.id AND z.id=t.canonical_result_id
  JOIN photo.artifacts a ON a.tenant_id=t.tenant_id
   AND a.id=(z.outputs->0->>'artifact_id')::uuid
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.operation='session.describe'
  ORDER BY (split_part(t.node_key,':',2))::integer;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v22_session_plan_inputs(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v22_session_plan_inputs(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v22_session_plan_inputs(uuid,uuid) TO photo_scheduler;
