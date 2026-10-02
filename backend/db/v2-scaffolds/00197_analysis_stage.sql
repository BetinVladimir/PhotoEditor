-- Materialize only initial describe shards. Transport closure publication and
-- seal occur later; all tasks remain blocked throughout this transaction.
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v22_analysis_describes(p_tenant uuid,p_run uuid,p_tasks jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 item jsonb; ordinal integer:=0; expected_count integer; expected_id uuid;
 h bytea; params photo.backend_documents%ROWTYPE; existing photo.tasks%ROWTYPE;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR jsonb_typeof(p_tasks) IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_tasks) NOT BETWEEN 1 AND 40
  OR pg_column_size(p_tasks)>262144 THEN RETURN false; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR r.status<>'draft'
  OR r.cancel_requested OR r.graph_sealed OR w.workflow_type<>'session'
  OR w.renderer_version<>'v2.2' OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
  OR (r.config_snapshot->>'project_id')::uuid IS DISTINCT FROM w.project_id
  OR NOT EXISTS(SELECT 1 FROM photo.quota_reservations q
   WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_run
    AND q.state='reserved' AND q.expires_at>clock_timestamp())
  OR NOT EXISTS(SELECT 1 FROM photo.snapshots s
   WHERE s.tenant_id=p_tenant AND s.id=r.input_snapshot_id AND s.kind='v2'
    AND s.principal_type='user' AND s.principal_id=r.created_by
    AND s.item_count=(r.config_snapshot->'v22_session_plan'->>'item_count')::integer
    AND s.digest=r.config_snapshot->'v22_session_plan'->>'snapshot_digest')
  OR NOT EXISTS(SELECT 1 FROM photo.tenants t WHERE t.id=p_tenant AND t.status='active')
  THEN RETURN false; END IF;
 expected_count:=((r.config_snapshot->'v22_session_plan'->>'item_count')::integer+127)/128;
 IF jsonb_array_length(p_tasks)<>expected_count THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_run
   AND d.purpose='v2_analysis_input_snapshot' AND d.state='ready'
   AND a.sha256=d.sha256 AND a.state='ready' AND a.object_version=d.object_version)
  OR NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_run
   AND d.purpose='v2_analysis_context' AND d.state='ready'
   AND d.sha256=r.config_snapshot->'v22_session_plan'->>'context_sha'
   AND a.sha256=d.sha256 AND a.state='ready' AND a.object_version=d.object_version)
  OR NOT EXISTS(SELECT 1 FROM photo.backend_documents d JOIN photo.artifacts a
  ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=p_run
   AND d.purpose='v2_analysis_consents' AND d.state='ready'
   AND a.sha256=d.sha256 AND a.state='ready' AND a.object_version=d.object_version)
  THEN RETURN false; END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant
  AND t.run_id=p_run AND t.operation<>'session.describe')
  OR EXISTS(SELECT 1 FROM photo.task_edges e WHERE e.tenant_id=p_tenant
   AND e.run_id=p_run) THEN RETURN false; END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  IF jsonb_typeof(item) IS DISTINCT FROM 'object'
   OR (item-'id'-'node_key'-'params_artifact_id'-'params_sha'
    -'input_digest'-'execution_profile')<>'{}'::jsonb
   OR item->>'node_key' IS DISTINCT FROM 'session.describe:'||ordinal
   OR coalesce(item->>'id','') !~
    '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->>'params_artifact_id','') !~
    '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   OR coalesce(item->>'params_sha','') !~ '^[0-9a-f]{64}$'
   OR coalesce(item->>'input_digest','') !~ '^[0-9a-f]{64}$'
   OR jsonb_typeof(item->'execution_profile') IS DISTINCT FROM 'object'
   OR item->'execution_profile' ? 'v2_input_manifest'
   OR coalesce(item->'execution_profile'->>'backend','') NOT IN ('cuda','cpu','mps')
   OR item->'execution_profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
   OR jsonb_typeof(item->'execution_profile'->'resource_estimate') IS DISTINCT FROM 'object'
   OR ((item->'execution_profile'->'resource_estimate')-'ram_bytes'-'vram_bytes'
    -'scratch_bytes'-'output_bytes'-'pixel_count')<>'{}'::jsonb
   OR NOT (item->'execution_profile'->'resource_estimate' ?&
    ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
   OR EXISTS(SELECT 1 FROM jsonb_each(item->'execution_profile'->'resource_estimate') x
    WHERE jsonb_typeof(x.value)<>'number' OR x.value::text !~ '^(0|[1-9][0-9]{0,15})$')
   OR (item->'execution_profile'->'resource_estimate'->>'ram_bytes')::bigint<536870912
   OR (item->'execution_profile'->'resource_estimate'->>'output_bytes')::bigint<1
   OR (item->'execution_profile'->'resource_estimate'->>'scratch_bytes')::bigint<1
   OR (item->'execution_profile'->>'backend'='cuda' AND
    (item->'execution_profile'->'resource_estimate'->>'vram_bytes')::bigint<1073741824)
   OR NOT EXISTS(SELECT 1 FROM photo.operation_registry op
    WHERE op.operation='session.describe' AND op.version='1'
     AND op.resource_class='gpu'
     AND op.backends ? (item->'execution_profile'->>'backend'))
   THEN RETURN false; END IF;
  h:=public.digest(uuid_send(w.id)||convert_to('session.describe:'||ordinal,'UTF8'),'sha1');
  h:=set_byte(h,6,(get_byte(h,6)&15)|80);
  h:=set_byte(h,8,(get_byte(h,8)&63)|128);
  expected_id:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
  IF (item->>'id')::uuid<>expected_id THEN RETURN false; END IF;
  SELECT * INTO params FROM photo.backend_documents d
   WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=expected_id
    AND d.purpose='v2_analysis_describe_params_'||ordinal;
  IF params.id IS NULL OR params.state<>'ready'
   OR params.artifact_id IS DISTINCT FROM (item->>'params_artifact_id')::uuid
   OR params.sha256 IS DISTINCT FROM item->>'params_sha'
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant
    AND a.id=params.artifact_id AND a.sha256=params.sha256 AND a.state='ready'
    AND a.object_version=params.object_version) THEN RETURN false; END IF;
  SELECT * INTO existing FROM photo.tasks t WHERE t.tenant_id=p_tenant
   AND t.run_id=p_run AND t.id=expected_id;
  IF existing.id IS NOT NULL THEN
   IF existing.node_key IS DISTINCT FROM item->>'node_key'
    OR existing.operation<>'session.describe' OR existing.operation_version<>'1'
    OR existing.resource_class<>'gpu' OR existing.state<>'blocked'
    OR existing.attempt<>0 OR existing.cancel_requested
    OR existing.params_artifact_id IS DISTINCT FROM params.artifact_id
    OR existing.input_digest IS DISTINCT FROM item->>'input_digest'
    OR existing.execution_profile IS DISTINCT FROM item->'execution_profile'
    THEN RETURN false; END IF;
  ELSE
   INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,
    operation_version,resource_class,inputs,params_artifact_id,
    input_digest,execution_profile)
   VALUES(expected_id,p_tenant,p_run,item->>'node_key','session.describe',
    '1','gpu','[]'::jsonb,params.artifact_id,item->>'input_digest',
    item->'execution_profile');
  END IF;
  ordinal:=ordinal+1;
 END LOOP;
 RETURN (SELECT count(*)=expected_count FROM photo.tasks t
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.stage_v22_analysis_describes(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v22_analysis_describes(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v22_analysis_describes(uuid,uuid,jsonb) TO photo_scheduler;
