-- Stage the late-bound plan node after its exact descriptor refs and params
-- document are ready. Input closure publication happens after this transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v22_session_plan(p_tenant uuid,p_run uuid,
 p_expansion uuid,p_token uuid,p_task uuid,p_params uuid,p_params_sha text,
 p_input_digest text,p_shards jsonb,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; e photo.graph_expansions%ROWTYPE;
 w photo.v2_workflows%ROWTYPE; existing photo.tasks%ROWTYPE;
 expected jsonb; expected_id uuid; h bytea; descriptor record; shard_count integer;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_expansion IS NULL OR p_token IS NULL
  OR p_task IS NULL OR p_params IS NULL OR p_params_sha !~ '^[0-9a-f]{64}$'
  OR p_input_digest !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_shards) IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_shards) NOT BETWEEN 1 AND 40
  OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object'
  OR p_profile->>'backend' IS DISTINCT FROM 'cpu'
  OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR p_profile ? 'v2_input_manifest' THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant
  AND id=p_expansion AND run_id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.graph_sealed
  OR r.cancel_requested OR e.id IS NULL OR e.kind<>'v2.session.plan/1'
  OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token
  OR e.expansion_lease_until<=clock_timestamp()
  OR w.id IS NULL OR w.workflow_type<>'session' OR w.renderer_version<>'v2.2'
  OR NOT EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant
   AND tenant.status='active') THEN RETURN false; END IF;
 h:=public.digest(uuid_send(w.id)||convert_to('session.plan:0','UTF8'),'sha1');
 h:=set_byte(h,6,(get_byte(h,6)&15)|80);
 h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF p_task<>expected_id OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op
  WHERE op.operation='session.plan' AND op.version='1'
   AND op.resource_class='cpu' AND op.backends ? 'cpu')
  OR NOT EXISTS(SELECT 1 FROM photo.backend_documents d
   JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
   WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_expansion
    AND d.purpose='v2_session_plan_params' AND d.state='ready'
    AND d.artifact_id=p_params AND d.sha256=p_params_sha
    AND a.sha256=d.sha256 AND a.state='ready' AND a.media_type='application/json'
    AND a.object_version=d.object_version AND a.verified_at IS NOT NULL
    AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false; END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',x.artifact_id,'sha256',x.sha256)
  ORDER BY x.pos),count(*) INTO expected,shard_count
 FROM photo.v22_session_plan_inputs(p_tenant,p_run) WITH ORDINALITY
  x(task_id,artifact_id,sha256,pos);
 IF shard_count<>jsonb_array_length(p_shards) OR expected IS DISTINCT FROM p_shards
  THEN RETURN false; END IF;
 SELECT * INTO existing FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run
  AND node_key='session.plan:0' FOR UPDATE;
 IF existing.id IS NOT NULL THEN
  RETURN existing.id=p_task AND existing.operation='session.plan'
   AND existing.operation_version='1' AND existing.resource_class='cpu'
   AND existing.params_artifact_id=p_params AND existing.input_digest=p_input_digest
   AND (existing.execution_profile-'v2_input_manifest'-'v2_entry_count'
    -'v2_entries_sha'-'contracts_sha')=p_profile AND existing.state='blocked';
 END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.operation NOT IN ('session.describe')) THEN RETURN false; END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,
  operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(p_task,p_tenant,p_run,'session.plan:0','session.plan','1','cpu',
  '[]'::jsonb,p_params,p_input_digest,p_profile);
 FOR descriptor IN SELECT task_id FROM photo.v22_session_plan_inputs(p_tenant,p_run) LOOP
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition)
  VALUES(p_tenant,p_run,descriptor.task_id,p_task,'success');
 END LOOP;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v22_session_plan(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v22_session_plan(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)
 FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v22_session_plan(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)
 TO photo_scheduler;
