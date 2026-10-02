-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_ingest_task(p_tenant uuid,p_run uuid,p_task jsonb,p_metadata uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;n photo.ingest_node_plans%ROWTYPE;old photo.tasks%ROWTYPE;
 predecessor uuid;params_id uuid;params_bytes bigint;source_bytes bigint;profile jsonb;estimate jsonb;v numeric;entry record;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task) IS DISTINCT FROM 'object' OR octet_length(p_task::text)>1048576
 OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR r.status NOT IN ('queued','running') OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO n FROM photo.ingest_node_plans WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF NOT FOUND OR p_task->>'node_key' IS DISTINCT FROM n.node_key OR p_task->>'operation' IS DISTINCT FROM n.operation
 OR p_task->>'operation_version' IS DISTINCT FROM '1' OR p_task->>'input_digest' IS NULL OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$'
 OR p_task->'inputs' IS DISTINCT FROM jsonb_build_array(jsonb_build_object('artifact_id',n.source_artifact_id::text,'sha256',n.source_sha)) THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation=n.operation AND op.version='1'
 AND op.resource_class=p_task->>'resource_class' AND op.timeout_seconds=(p_task->>'timeout_seconds')::integer) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;
 SELECT a.bytes INTO params_bytes FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id
 WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.state='ready' AND a.sha256=n.params_sha AND a.sha256=p_task->'params'->>'sha256'
 AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT a.bytes INTO source_bytes FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=n.source_artifact_id
 AND a.sha256=n.source_sha AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF NOT FOUND THEN RETURN false;END IF;
 predecessor:=NULL;
 IF n.operation<>'asset.verify' THEN
  SELECT t.id INTO predecessor FROM photo.ingest_node_plans pn JOIN photo.tasks t ON t.tenant_id=pn.tenant_id AND t.run_id=pn.run_id AND t.id=pn.id
  WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.ordinal=n.ordinal
  AND pn.operation=CASE n.operation WHEN 'asset.inspect' THEN 'asset.verify' ELSE 'asset.inspect' END;
  IF NOT FOUND THEN RETURN false;END IF;
 END IF;
 IF n.operation='asset.decode' THEN
  IF p_metadata IS NULL OR NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.tasks t ON t.tenant_id=a.tenant_id AND t.id=a.created_by_task_id
   JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   WHERE a.tenant_id=p_tenant AND a.id=p_metadata AND a.state='ready' AND a.output_role='metadata'
   AND t.id=predecessor AND t.state='succeeded'
   AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))) THEN RETURN false;END IF;
 ELSIF p_metadata IS NOT NULL THEN RETURN false;
 END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';
 IF profile->>'backend' IS DISTINCT FROM 'cpu' OR profile->>'precision' IS DISTINCT FROM 'float32'
 OR jsonb_typeof(estimate) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(estimate))<>5
 OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count']) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_each(estimate) LOOP
  IF jsonb_typeof(entry.value)<>'number' THEN RETURN false;END IF;
  v:=entry.value::text::numeric;IF v<0 OR v>9007199254740991 OR v<>trunc(v) THEN RETURN false;END IF;
 END LOOP;
 IF (estimate->>'ram_bytes')::numeric<1073741824 OR (estimate->>'vram_bytes')::numeric<>0
 OR (estimate->>'pixel_count')::numeric NOT BETWEEN 1 AND 200000000
 OR (estimate->>'output_bytes')::numeric<1048576
 OR (estimate->>'scratch_bytes')::numeric<source_bytes+params_bytes+(estimate->>'output_bytes')::numeric THEN RETURN false;END IF;
 IF n.operation='asset.decode' AND (estimate->>'output_bytes')::numeric<12*(estimate->>'pixel_count')::numeric+2097152 THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;
 IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=n.node_key AND old.operation=n.operation AND old.params_artifact_id=params_id
 AND old.input_digest=p_task->>'input_digest' AND old.inputs=p_task->'inputs' AND old.execution_profile=profile;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(n.id,p_tenant,p_run,n.node_key,n.operation,'1',p_task->>'resource_class','blocked',p_task->'inputs',params_id,p_task->>'input_digest',profile);
 IF predecessor IS NOT NULL THEN INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition)
 VALUES(p_tenant,p_run,predecessor,n.id,'success');END IF;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_ingest_task(uuid,uuid,jsonb,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_ingest_task(uuid,uuid,jsonb,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_ingest_task(uuid,uuid,jsonb,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
