-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.materialize_ingest_task(p_tenant uuid,p_run uuid,p_task jsonb,p_metadata uuid) RETURNS boolean
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
  IF NOT EXISTS(SELECT FROM photo.ingest_progress p JOIN photo.ingest_decode_items item ON item.tenant_id=p.tenant_id AND item.run_id=p.run_id
   WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.phase='decode_build' AND item.ordinal=n.ordinal
   AND item.outcome='decode' AND item.metadata_id=p_metadata) THEN RETURN false;END IF;
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.pending_ingest_activations(p_limit integer)
RETURNS TABLE(task_id uuid,params_id uuid,input_digest text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'INVALID_LIMIT' USING ERRCODE='22023';END IF;
 RETURN QUERY SELECT t.id,t.params_artifact_id,t.input_digest
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.ingest_progress p ON p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.phase IN ('inspection','decode')
 JOIN photo.ingest_node_plans n ON n.tenant_id=t.tenant_id AND n.run_id=t.run_id AND n.id=t.id AND n.operation=t.operation
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.id=t.params_artifact_id AND a.state='ready' AND a.sha256=n.params_sha
 JOIN photo.artifacts source ON source.tenant_id=n.tenant_id AND source.id=n.source_artifact_id AND source.state='ready' AND source.sha256=n.source_sha
 WHERE r.kind='ingest' AND r.graph_sealed AND NOT r.cancel_requested AND r.status IN ('queued','running')
 AND t.state='blocked' AND NOT t.cancel_requested AND t.terminal_reason IS NULL AND ((p.phase='inspection' AND t.operation IN ('asset.verify','asset.inspect')) OR (p.phase='decode' AND t.operation='asset.decode'))
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.state<>'closed')
 AND NOT EXISTS(SELECT FROM photo.task_edges e JOIN photo.tasks predecessor ON predecessor.tenant_id=e.tenant_id AND predecessor.id=e.prerequisite_id
  WHERE e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.dependent_id=t.id
  AND (e.condition<>'success' OR predecessor.state<>'succeeded' OR predecessor.canonical_result_id IS NULL))
 ORDER BY r.created_at,r.id,n.ordinal,n.operation LIMIT p_limit;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.pending_ingest_activations(integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.pending_ingest_activations(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.pending_ingest_activations(integer) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.seal_ingest_decode(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;p photo.ingest_progress%ROWTYPE;successful bigint;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'ingest' OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN RETURN false;END IF;
 SELECT * INTO p FROM photo.ingest_progress WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF p.phase IN ('decode','catalog') THEN RETURN r.graph_sealed;END IF;
 IF p.phase<>'decode_build' OR r.graph_sealed OR r.revision>=9007199254740991 THEN RETURN false;END IF;
 IF (SELECT count(*) FROM photo.ingest_decode_items WHERE tenant_id=p_tenant AND run_id=p_run)<>p.item_count THEN RETURN false;END IF;
 SELECT count(*) INTO successful FROM photo.ingest_decode_items WHERE tenant_id=p_tenant AND run_id=p_run AND outcome='decode';
 IF (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run)<>2*p.item_count+successful
 OR (SELECT count(*) FROM photo.task_edges WHERE tenant_id=p_tenant AND run_id=p_run)<>p.item_count+successful THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.ingest_decode_items item
  WHERE item.tenant_id=p_tenant AND item.run_id=p_run AND item.outcome='decode' AND NOT EXISTS(
   SELECT FROM photo.ingest_node_plans n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id
   JOIN photo.ingest_node_plans pn ON pn.tenant_id=n.tenant_id AND pn.run_id=n.run_id AND pn.ordinal=n.ordinal AND pn.operation='asset.inspect'
   JOIN photo.tasks parent ON parent.tenant_id=pn.tenant_id AND parent.id=pn.id AND parent.run_id=pn.run_id
   JOIN photo.task_results z ON z.tenant_id=parent.tenant_id AND z.task_id=parent.id AND z.id=parent.canonical_result_id
   JOIN photo.artifacts metadata ON metadata.tenant_id=item.tenant_id AND metadata.id=item.metadata_id AND metadata.sha256=item.metadata_sha
   WHERE n.tenant_id=item.tenant_id AND n.run_id=item.run_id AND n.ordinal=item.ordinal AND n.operation='asset.decode'
   AND t.operation='asset.decode' AND t.state='blocked' AND NOT t.cancel_requested AND t.terminal_reason IS NULL
   AND parent.state='succeeded' AND metadata.state='ready' AND metadata.created_by_task_id=parent.id AND metadata.output_role='metadata'
   AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',metadata.id::text,'sha256',metadata.sha256))
   AND EXISTS(SELECT FROM photo.task_edges e WHERE e.tenant_id=t.tenant_id AND e.run_id=t.run_id AND e.prerequisite_id=parent.id AND e.dependent_id=t.id AND e.condition='success')
  )) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation IN ('asset.verify','asset.inspect') AND state NOT IN ('succeeded','failed','cancelled','skipped'))
 OR EXISTS(SELECT FROM photo.graph_expansions WHERE tenant_id=p_tenant AND run_id=p_run AND state<>'closed') THEN RETURN false;END IF;
 UPDATE photo.ingest_progress SET phase=CASE WHEN successful=0 THEN 'catalog' ELSE 'decode' END WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_ingest_decode(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_ingest_decode(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_ingest_decode(uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
