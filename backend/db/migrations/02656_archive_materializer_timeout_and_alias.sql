-- Forward-only: archive materializer alias and persisted registry timeout.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 2655 THEN
 RAISE EXCEPTION 'ARCHIVE_2656_HEAD_PREIMAGE';END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $guard$ DECLARE before_meta jsonb;after_meta jsonb;BEGIN
 SELECT to_jsonb(p) INTO before_meta FROM pg_proc p WHERE oid='photo.materialize_download_archive_task(uuid,uuid,jsonb)'::regprocedure;
 IF before_meta->>'prosrc' IS DISTINCT FROM $old$
DECLARE r photo.runs%ROWTYPE;n photo.download_archive_plans%ROWTYPE;old photo.tasks%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;producer text;entry record;value numeric;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object' OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO n FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF r.id IS NULL OR n.id IS NULL OR r.graph_sealed OR r.cancel_requested OR r.status<>'running' OR p_task->>'node_key'<>n.node_key OR p_task->>'operation'<>'archive.build'
 OR jsonb_array_length(n.inputs)>512 OR p_task->>'operation_version'<>'1' OR p_task->'inputs'<>n.inputs OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation='archive.build' AND op.version='1' AND op.resource_class=p_task->>'resource_class' AND op.timeout_seconds=(p_task->>'timeout_seconds')::integer) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;
 IF NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.state='ready'
 AND a.sha256=n.params_sha AND a.sha256=p_task->'params'->>'sha256' AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready') THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';IF profile->>'backend'<>'cpu' OR profile->>'precision'<>'byte' OR jsonb_typeof(estimate)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(estimate))<>5 OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate->>'ram_bytes')::numeric<1073741824 OR (estimate->>'vram_bytes')::numeric<>0 OR (estimate->>'scratch_bytes')::numeric<1 OR (estimate->>'output_bytes')::numeric<1 OR (estimate->>'pixel_count')::numeric<>0 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_each(estimate) LOOP IF jsonb_typeof(entry.value)<>'number' THEN RETURN false;END IF;value:=entry.value::text::numeric;IF value<0 OR value>9007199254740991 OR value<>trunc(value) THEN RETURN false;END IF;END LOOP;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;IF FOUND THEN RETURN old.run_id=p_run AND old.inputs=n.inputs AND old.params_artifact_id=params_id AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(n.id,p_tenant,p_run,n.node_key,'archive.build','1','archive','blocked',n.inputs,params_id,p_task->>'input_digest',profile);
 FOR producer IN SELECT jsonb_array_elements_text(n.producer_tasks) LOOP
  IF NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=producer::uuid AND t.operation='export.encode' AND t.state='succeeded') THEN RAISE EXCEPTION 'ARCHIVE_PRODUCER_INVALID' USING ERRCODE='23514';END IF;
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(p_tenant,p_run,producer::uuid,n.id,'success');
 END LOOP;RETURN true;
END $old$ THEN RAISE EXCEPTION 'ARCHIVE_TIMEOUT_PREIMAGE';END IF;
 EXECUTE $ddl$CREATE OR REPLACE FUNCTION photo.materialize_download_archive_task(p_tenant uuid, p_run uuid, p_task jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE r photo.runs%ROWTYPE;n photo.download_archive_plans%ROWTYPE;old photo.tasks%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;producer text;entry record;value numeric;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object' OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO n FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF r.id IS NULL OR n.id IS NULL OR r.graph_sealed OR r.cancel_requested OR r.status<>'running' OR p_task->>'node_key'<>n.node_key OR p_task->>'operation'<>'archive.build'
 OR jsonb_array_length(n.inputs)>512 OR p_task->>'operation_version'<>'1' OR p_task->'inputs'<>n.inputs OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation='archive.build' AND op.version='1' AND op.resource_class=p_task->>'resource_class' AND op.timeout_seconds=(p_task->>'timeout_seconds')::integer) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;
 IF NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.state='ready'
 AND a.sha256=n.params_sha AND a.sha256=p_task->'params'->>'sha256' AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready') THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';IF profile->>'backend'<>'cpu' OR profile->>'precision'<>'byte' OR jsonb_typeof(estimate)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(estimate))<>5 OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate->>'ram_bytes')::numeric<1073741824 OR (estimate->>'vram_bytes')::numeric<>0 OR (estimate->>'scratch_bytes')::numeric<1 OR (estimate->>'output_bytes')::numeric<1 OR (estimate->>'pixel_count')::numeric<>0 THEN RETURN false;END IF;
 FOR entry IN SELECT fields.value FROM jsonb_each(estimate) AS fields LOOP IF jsonb_typeof(entry.value)<>'number' THEN RETURN false;END IF;value:=entry.value::text::numeric;IF value<0 OR value>9007199254740991 OR value<>trunc(value) THEN RETURN false;END IF;END LOOP;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;IF FOUND THEN RETURN old.run_id=p_run AND old.inputs=n.inputs AND old.params_artifact_id=params_id AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile AND old.timeout_seconds=(p_task->>'timeout_seconds')::integer;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds)
 VALUES(n.id,p_tenant,p_run,n.node_key,'archive.build','1','archive','blocked',n.inputs,params_id,p_task->>'input_digest',profile,(p_task->>'timeout_seconds')::integer);
 FOR producer IN SELECT jsonb_array_elements_text(n.producer_tasks) LOOP
  IF NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=producer::uuid AND t.operation='export.encode' AND t.state='succeeded') THEN RAISE EXCEPTION 'ARCHIVE_PRODUCER_INVALID' USING ERRCODE='23514';END IF;
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(p_tenant,p_run,producer::uuid,n.id,'success');
 END LOOP;RETURN true;
END $function$
$ddl$;
 SELECT to_jsonb(p) INTO after_meta FROM pg_proc p WHERE oid='photo.materialize_download_archive_task(uuid,uuid,jsonb)'::regprocedure;
 IF (before_meta-'prosrc') IS DISTINCT FROM (after_meta-'prosrc') OR after_meta->>'prosrc' IS DISTINCT FROM $new$
DECLARE r photo.runs%ROWTYPE;n photo.download_archive_plans%ROWTYPE;old photo.tasks%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;producer text;entry record;value numeric;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object' OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO n FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF r.id IS NULL OR n.id IS NULL OR r.graph_sealed OR r.cancel_requested OR r.status<>'running' OR p_task->>'node_key'<>n.node_key OR p_task->>'operation'<>'archive.build'
 OR jsonb_array_length(n.inputs)>512 OR p_task->>'operation_version'<>'1' OR p_task->'inputs'<>n.inputs OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation='archive.build' AND op.version='1' AND op.resource_class=p_task->>'resource_class' AND op.timeout_seconds=(p_task->>'timeout_seconds')::integer) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;
 IF NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.state='ready'
 AND a.sha256=n.params_sha AND a.sha256=p_task->'params'->>'sha256' AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready') THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';IF profile->>'backend'<>'cpu' OR profile->>'precision'<>'byte' OR jsonb_typeof(estimate)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(estimate))<>5 OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate->>'ram_bytes')::numeric<1073741824 OR (estimate->>'vram_bytes')::numeric<>0 OR (estimate->>'scratch_bytes')::numeric<1 OR (estimate->>'output_bytes')::numeric<1 OR (estimate->>'pixel_count')::numeric<>0 THEN RETURN false;END IF;
 FOR entry IN SELECT fields.value FROM jsonb_each(estimate) AS fields LOOP IF jsonb_typeof(entry.value)<>'number' THEN RETURN false;END IF;value:=entry.value::text::numeric;IF value<0 OR value>9007199254740991 OR value<>trunc(value) THEN RETURN false;END IF;END LOOP;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;IF FOUND THEN RETURN old.run_id=p_run AND old.inputs=n.inputs AND old.params_artifact_id=params_id AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile AND old.timeout_seconds=(p_task->>'timeout_seconds')::integer;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds)
 VALUES(n.id,p_tenant,p_run,n.node_key,'archive.build','1','archive','blocked',n.inputs,params_id,p_task->>'input_digest',profile,(p_task->>'timeout_seconds')::integer);
 FOR producer IN SELECT jsonb_array_elements_text(n.producer_tasks) LOOP
  IF NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=producer::uuid AND t.operation='export.encode' AND t.state='succeeded') THEN RAISE EXCEPTION 'ARCHIVE_PRODUCER_INVALID' USING ERRCODE='23514';END IF;
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(p_tenant,p_run,producer::uuid,n.id,'success');
 END LOOP;RETURN true;
END $new$ THEN RAISE EXCEPTION 'ARCHIVE_TIMEOUT_METADATA_DRIFT';END IF;
END $guard$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
