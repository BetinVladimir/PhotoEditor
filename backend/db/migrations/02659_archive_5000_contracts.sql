-- STAGED PROPOSAL ONLY; canonical02659 currently unused. No installation approval.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN IF (SELECT max(version_id)FROM public.goose_db_version WHERE is_applied)IS DISTINCT FROM 2658 THEN RAISE EXCEPTION 'ARCHIVE5000_HEAD_PREIMAGE';END IF;END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $document$ DECLARE c pg_constraint%ROWTYPE;expression text;BEGIN
 IF EXISTS(SELECT FROM pg_attribute WHERE attrelid='photo.backend_documents'::regclass AND attname='archive_plan_id'AND NOT attisdropped) THEN RAISE EXCEPTION 'ARCHIVE5000_DOCUMENT_COLUMN_PREIMAGE';END IF;
 SELECT * INTO c FROM pg_constraint WHERE conrelid='photo.backend_documents'::regclass AND conname='backend_documents_bytes_check';
 IF NOT FOUND OR NOT c.convalidated OR pg_get_constraintdef(c.oid,true) IS DISTINCT FROM $expected$CHECK (bytes >= 2 AND bytes <=
CASE
    WHEN purpose = ANY (ARRAY['v2_analysis_input_snapshot'::text, 'v2_analysis_consents'::text, 'v23_training_manifest'::text]) THEN 8388608
    ELSE 1048576
END OR purpose = 'v30_delivery_snapshot'::text AND bytes >= 2 AND bytes <= 8388608 OR purpose = 'v31_signing_lineage'::text AND bytes >= 2 AND bytes <= 8388608 OR purpose = 'v31_signing_policy'::text AND bytes >= 2 AND bytes <= 1048576)$expected$ THEN RAISE EXCEPTION 'ARCHIVE5000_DOCUMENT_CONSTRAINT_PREIMAGE';END IF;
 expression:=pg_get_expr(c.conbin,c.conrelid);
 ALTER TABLE photo.backend_documents ADD COLUMN archive_plan_id uuid;
 ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_archive_plan_fk FOREIGN KEY(tenant_id,archive_plan_id)REFERENCES photo.download_archive_plans(tenant_id,id) DEFERRABLE INITIALLY DEFERRED;
 ALTER TABLE photo.backend_documents DROP CONSTRAINT backend_documents_bytes_check;
 EXECUTE format('ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_bytes_check CHECK ((archive_plan_id IS NULL AND (%s)) OR (archive_plan_id IS NOT NULL AND purpose=''task_params'' AND owner_id=archive_plan_id AND bytes BETWEEN 2 AND 8388608))',expression);
END $document$;
-- +goose StatementEnd
-- Retain NOT VALID: historical rows remain immutable; new writes allow at most5000.
-- +goose StatementBegin
DO $constraint$ DECLARE c pg_constraint%ROWTYPE;BEGIN
 SELECT * INTO c FROM pg_constraint WHERE conrelid='photo.download_archive_plans'::regclass AND conname='download_archive_transport_inputs';
 IF NOT FOUND OR c.contype<>'c' OR c.convalidated OR pg_get_constraintdef(c.oid) IS DISTINCT FROM 'CHECK (((jsonb_array_length(inputs) >= 1) AND (jsonb_array_length(inputs) <= 512))) NOT VALID' THEN RAISE EXCEPTION 'ARCHIVE5000_CONSTRAINT_PREIMAGE';END IF;
 ALTER TABLE photo.download_archive_plans DROP CONSTRAINT download_archive_transport_inputs;
 ALTER TABLE photo.download_archive_plans ADD CONSTRAINT download_archive_transport_inputs CHECK(jsonb_array_length(inputs) BETWEEN 1 AND 5000) NOT VALID;
END $constraint$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $migration$ DECLARE old_meta jsonb;new_meta jsonb;BEGIN
 IF pg_get_functiondef('photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb)'::regprocedure) IS DISTINCT FROM $preimage$CREATE OR REPLACE FUNCTION photo.register_download_archive_plan(p_tenant uuid, p_run uuid, p_id uuid, p_volume integer, p_key text, p_inputs jsonb, p_producers jsonb, p_params_sha text, p_params jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE r photo.runs%ROWTYPE;b photo.download_build_state%ROWTYPE;old photo.download_archive_plans%ROWTYPE;expected_inputs jsonb;expected_producers jsonb;expected_params jsonb;
BEGIN
 IF p_id IS NULL OR p_volume NOT BETWEEN 1 AND 5000 OR p_key IS DISTINCT FROM 'archive.build/'||lpad(p_volume::text,4,'0')
 OR jsonb_typeof(p_inputs)<>'array' OR jsonb_array_length(p_inputs) NOT BETWEEN 1 AND 512 OR jsonb_typeof(p_producers)<>'array' OR jsonb_array_length(p_producers)<>jsonb_array_length(p_inputs)
 OR p_params_sha !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_params)<>'object' OR octet_length(p_params::text)>1048576
 OR (p_params-'snapshot_id'-'snapshot_digest'-'volume_no'-'entries')<>'{}' OR (p_params->>'volume_no')::integer<>p_volume THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR b.run_id IS NULL OR b.phase<>'packaging' OR r.graph_sealed OR r.cancel_requested OR r.status<>'running' THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256) ORDER BY i.ordinal),jsonb_agg(p.id::text ORDER BY i.ordinal)
 INTO expected_inputs,expected_producers FROM photo.download_jobs j JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id
 JOIN photo.download_export_plans p ON p.tenant_id=i.tenant_id AND p.run_id=p_run AND p.ordinal=i.ordinal
 WHERE j.tenant_id=p_tenant AND j.run_id=p_run AND i.volume_no=p_volume AND i.delivery_kind='zip';
 SELECT jsonb_build_object('snapshot_id',s.id::text,'snapshot_digest',s.digest,'volume_no',p_volume,'entries',
  jsonb_agg(jsonb_build_object('artifact',jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256),'bytes',i.bytes,'entry_name',i.entry_name) ORDER BY i.ordinal)) INTO expected_params
 FROM photo.download_jobs j JOIN photo.snapshots s ON s.tenant_id=j.tenant_id AND s.id=j.snapshot_id
 JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id
 WHERE j.tenant_id=p_tenant AND j.run_id=p_run AND i.volume_no=p_volume AND i.delivery_kind='zip' GROUP BY s.id,s.digest;
 IF expected_inputs IS DISTINCT FROM p_inputs OR expected_producers IS DISTINCT FROM p_producers OR expected_params IS DISTINCT FROM p_params THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run AND volume_no=p_volume;
 IF FOUND THEN RETURN old.id=p_id AND old.node_key=p_key AND old.inputs=p_inputs AND old.producer_tasks=p_producers AND old.params_sha=p_params_sha AND old.params=p_params;END IF;
 IF EXISTS(SELECT FROM photo.download_archive_plans WHERE id=p_id) THEN RETURN false;END IF;
 INSERT INTO photo.download_archive_plans(id,tenant_id,run_id,volume_no,node_key,inputs,producer_tasks,params_sha,params)
 VALUES(p_id,p_tenant,p_run,p_volume,p_key,p_inputs,p_producers,p_params_sha,p_params);RETURN true;
END $function$
$preimage$ THEN RAISE EXCEPTION 'ARCHIVE5000_FUNCTION_PREIMAGE_0';END IF;
 SELECT to_jsonb(p)INTO old_meta FROM pg_proc p WHERE oid='photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb)'::regprocedure;
 EXECUTE $definition$CREATE OR REPLACE FUNCTION photo.register_download_archive_plan(p_tenant uuid, p_run uuid, p_id uuid, p_volume integer, p_key text, p_inputs jsonb, p_producers jsonb, p_params_sha text, p_params jsonb)
 RETURNS boolean
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE r photo.runs%ROWTYPE;b photo.download_build_state%ROWTYPE;old photo.download_archive_plans%ROWTYPE;expected_inputs jsonb;expected_producers jsonb;expected_params jsonb;
BEGIN
 IF p_id IS NULL OR p_volume NOT BETWEEN 1 AND 5000 OR p_key IS DISTINCT FROM 'archive.build/'||lpad(p_volume::text,4,'0')
 OR jsonb_typeof(p_inputs)<>'array' OR jsonb_array_length(p_inputs) NOT BETWEEN 1 AND 5000 OR jsonb_typeof(p_producers)<>'array' OR jsonb_array_length(p_producers)<>jsonb_array_length(p_inputs)
 OR p_params_sha !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_params)<>'object' OR octet_length(p_params::text)>8388608
 OR (p_params-'snapshot_id'-'snapshot_digest'-'volume_no'-'entries')<>'{}' OR (p_params->>'volume_no')::integer<>p_volume THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR b.run_id IS NULL OR b.phase<>'packaging' OR r.graph_sealed OR r.cancel_requested OR r.status<>'running' THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256) ORDER BY i.ordinal),jsonb_agg(p.id::text ORDER BY i.ordinal)
 INTO expected_inputs,expected_producers FROM photo.download_jobs j JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id
 JOIN photo.download_export_plans p ON p.tenant_id=i.tenant_id AND p.run_id=p_run AND p.ordinal=i.ordinal
 WHERE j.tenant_id=p_tenant AND j.run_id=p_run AND i.volume_no=p_volume AND i.delivery_kind='zip';
 SELECT jsonb_build_object('snapshot_id',s.id::text,'snapshot_digest',s.digest,'volume_no',p_volume,'entries',
  jsonb_agg(jsonb_build_object('artifact',jsonb_build_object('artifact_id',i.artifact_id::text,'sha256',i.sha256),'bytes',i.bytes,'entry_name',i.entry_name) ORDER BY i.ordinal)) INTO expected_params
 FROM photo.download_jobs j JOIN photo.snapshots s ON s.tenant_id=j.tenant_id AND s.id=j.snapshot_id
 JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id
 WHERE j.tenant_id=p_tenant AND j.run_id=p_run AND i.volume_no=p_volume AND i.delivery_kind='zip' GROUP BY s.id,s.digest;
 IF expected_inputs IS DISTINCT FROM p_inputs OR expected_producers IS DISTINCT FROM p_producers OR expected_params IS DISTINCT FROM p_params THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run AND volume_no=p_volume;
 IF FOUND THEN RETURN old.id=p_id AND old.node_key=p_key AND old.inputs=p_inputs AND old.producer_tasks=p_producers AND old.params_sha=p_params_sha AND old.params=p_params;END IF;
 IF EXISTS(SELECT FROM photo.download_archive_plans WHERE id=p_id) THEN RETURN false;END IF;
 INSERT INTO photo.download_archive_plans(id,tenant_id,run_id,volume_no,node_key,inputs,producer_tasks,params_sha,params)
 VALUES(p_id,p_tenant,p_run,p_volume,p_key,p_inputs,p_producers,p_params_sha,p_params);RETURN true;
END $function$
$definition$;
 SELECT to_jsonb(p)INTO new_meta FROM pg_proc p WHERE oid='photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb)'::regprocedure;
 IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' THEN RAISE EXCEPTION 'ARCHIVE5000_METADATA_DRIFT_0';END IF;
 END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $migration$ DECLARE old_meta jsonb;new_meta jsonb;BEGIN
 IF pg_get_functiondef('photo.materialize_download_archive_task(uuid,uuid,jsonb)'::regprocedure) IS DISTINCT FROM $preimage$CREATE OR REPLACE FUNCTION photo.materialize_download_archive_task(p_tenant uuid, p_run uuid, p_task jsonb)
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
$preimage$ THEN RAISE EXCEPTION 'ARCHIVE5000_FUNCTION_PREIMAGE_1';END IF;
 SELECT to_jsonb(p)INTO old_meta FROM pg_proc p WHERE oid='photo.materialize_download_archive_task(uuid,uuid,jsonb)'::regprocedure;
 EXECUTE $definition$CREATE OR REPLACE FUNCTION photo.materialize_download_archive_task(p_tenant uuid, p_run uuid, p_task jsonb)
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
 OR jsonb_array_length(n.inputs)>5000 OR p_task->>'operation_version'<>'1' OR p_task->'inputs'<>n.inputs OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation='archive.build' AND op.version='1' AND op.resource_class=p_task->>'resource_class' AND op.timeout_seconds=(p_task->>'timeout_seconds')::integer) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;
 IF NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.state='ready'
 AND a.sha256=n.params_sha AND a.sha256=p_task->'params'->>'sha256' AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready') THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=n.node_key AND old.operation='archive.build' AND old.operation_version='1' AND old.resource_class='archive' AND old.inputs=n.inputs AND old.params_artifact_id=params_id AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile AND old.timeout_seconds=(p_task->>'timeout_seconds')::integer;END IF;
 estimate:=profile->'resource_estimate';IF profile->>'backend'<>'cpu' OR profile->>'precision'<>'byte' OR jsonb_typeof(estimate)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(estimate))<>5 OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate->>'ram_bytes')::numeric<1073741824 OR (estimate->>'vram_bytes')::numeric<>0 OR (estimate->>'scratch_bytes')::numeric<(SELECT sum((e->>'bytes')::numeric)+16777216+4096*jsonb_array_length(n.inputs) FROM jsonb_array_elements(n.params->'entries')e) OR (estimate->>'output_bytes')::numeric<(SELECT sum((e->>'bytes')::numeric)+16777216+4096*jsonb_array_length(n.inputs) FROM jsonb_array_elements(n.params->'entries')e) OR (estimate->>'pixel_count')::numeric<>0 THEN RETURN false;END IF;
 FOR entry IN SELECT fields.value FROM jsonb_each(estimate) AS fields LOOP IF jsonb_typeof(entry.value)<>'number' THEN RETURN false;END IF;value:=entry.value::text::numeric;IF value<0 OR value>9007199254740991 OR value<>trunc(value) THEN RETURN false;END IF;END LOOP;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds)
 VALUES(n.id,p_tenant,p_run,n.node_key,'archive.build','1','archive','blocked',n.inputs,params_id,p_task->>'input_digest',profile,(p_task->>'timeout_seconds')::integer);
 FOR producer IN SELECT jsonb_array_elements_text(n.producer_tasks) LOOP
  IF NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=producer::uuid AND t.operation='export.encode' AND t.state='succeeded') THEN RAISE EXCEPTION 'ARCHIVE_PRODUCER_INVALID' USING ERRCODE='23514';END IF;
  INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(p_tenant,p_run,producer::uuid,n.id,'success');
 END LOOP;RETURN true;
END $function$
$definition$;
 SELECT to_jsonb(p)INTO new_meta FROM pg_proc p WHERE oid='photo.materialize_download_archive_task(uuid,uuid,jsonb)'::regprocedure;
 IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' THEN RAISE EXCEPTION 'ARCHIVE5000_METADATA_DRIFT_1';END IF;
 END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $migration$ DECLARE old_meta jsonb;new_meta jsonb;BEGIN
 IF pg_get_functiondef('photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure) IS DISTINCT FROM $preimage$CREATE OR REPLACE FUNCTION photo.reserve_dispatch_resources(p_task uuid, p_node text, p_ram bigint, p_vram bigint, p_scratch bigint, p_event uuid, p_correlation uuid, p_trace text)
 RETURNS TABLE(outcome text, command jsonb)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE n photo.resource_nodes%ROWTYPE; t photo.tasks%ROWTYPE; hint photo.tasks%ROWTYPE; r photo.runs%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; op photo.operation_registry%ROWTYPE; params photo.artifacts%ROWTYPE;
 used_ram numeric; used_vram numeric; used_scratch numeric; used_gpu integer; quota_gpu integer; until_time timestamptz;
 envelope jsonb; model jsonb; backend text; model_lock text; input jsonb; ai photo.artifacts%ROWTYPE;
 mask_dispatch_domain jsonb;
BEGIN
 IF p_task IS NULL OR p_event IS NULL OR p_correlation IS NULL OR p_node IS NULL OR p_trace IS NULL OR length(p_trace)>128
 OR p_ram IS NULL OR p_vram IS NULL OR p_scratch IS NULL OR p_ram<=0 OR p_vram<0 OR p_scratch<0 THEN
 RAISE EXCEPTION 'INVALID_RESERVATION' USING ERRCODE='22023'; END IF;
 SELECT * INTO hint FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::jsonb; RETURN; END IF;

 mask_dispatch_domain:=photo.authored_execution_parent_domain(p_task);
 PERFORM photo.authored_mask_lock_execution_parent(p_task);
 IF photo.authored_execution_parent_domain(p_task) IS DISTINCT FROM mask_dispatch_domain THEN
  RAISE EXCEPTION 'AUTHORED_MASK_DISPATCH_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE SKIP LOCKED;
 IF FOUND AND photo.authored_execution_parent_domain(p_task) IS DISTINCT FROM mask_dispatch_domain THEN
  RAISE EXCEPTION 'AUTHORED_MASK_DISPATCH_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;

 IF NOT FOUND OR NOT n.enabled OR NOT photo.capacity_policy_node_fresh(n) OR n.doctor_report_sha256 IS NULL THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 p_scratch:=photo.capacity_policy_scratch_reservation(n,p_scratch);
 IF p_scratch IS NULL THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE node_id=p_node AND resource_class=hint.resource_class AND task_id IS NULL ORDER BY slot_name LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_USAGE_MISSING'; END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE id=p_task FOR UPDATE;
 IF t.state<>'ready' OR t.attempt>=6 OR t.tenant_id<>hint.tenant_id OR t.run_id<>hint.run_id OR t.resource_class<>slot.resource_class THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 IF t.cancel_requested OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN
 RETURN QUERY SELECT 'cancelled',NULL::jsonb; RETURN; END IF;
 IF NOT r.graph_sealed THEN RETURN QUERY SELECT 'unsealed',NULL::jsonb; RETURN; END IF;
 SELECT * INTO op FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version;
 IF NOT FOUND OR op.resource_class<>t.resource_class THEN RAISE EXCEPTION 'OPERATION_MISMATCH' USING ERRCODE='22023'; END IF;
 backend:=t.execution_profile->>'backend';
 IF backend IS NULL OR NOT op.backends ? backend THEN RETURN QUERY SELECT 'unsupported_backend',NULL::jsonb; RETURN; END IF;
 model:=NULL;
 IF op.model_id IS NOT NULL THEN
  model_lock:=t.execution_profile->>'model_lock_id';
  -- Identified native model profiles pin model_lock_sha. Never rewrite old tasks.
  IF t.operation IN('mask.person','face.detect','face.landmarks','face.parse','scene.parse','depth.predict') AND t.operation_version='1'
   AND t.execution_profile->>'renderer_version'='v2.2' THEN
   IF model_lock IS NOT NULL AND model_lock IS DISTINCT FROM t.execution_profile->>'model_lock_sha' THEN
    RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN;
   END IF;
   model_lock:=t.execution_profile->>'model_lock_sha';
  END IF;

  IF model_lock IS NULL OR model_lock !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN; END IF;
  model:=jsonb_build_object('id',op.model_id,'lock_id',model_lock);
 END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled
 AND photo.authored_render_worker_pair_ready(t.id,w.worker_id)
 AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class AND w.backends ? backend AND (w.qualified_operations->t.operation) ? backend
   AND (t.operation<>'series.balance' OR
    t.execution_profile->>'device_profile_sha' IS NULL OR
    photo.v23_series_worker_device_matches(t.tenant_id,w.worker_id,t.execution_profile))
 AND photo.v31_signer_worker_matches(t.tenant_id,w.worker_id,t.operation,t.execution_profile) AND photo.capacity_policy_worker_fresh(w) AND (op.model_id IS NOT NULL AND w.lock_id=model_lock OR
  op.model_id IS NULL AND (t.operation<>'series.balance' OR
   t.execution_profile->>'model_lock_id' IS NULL OR
   w.lock_id=t.execution_profile->>'model_lock_id'))) THEN
 RETURN QUERY SELECT 'waiting_worker',NULL::jsonb; RETURN; END IF;
 SELECT coalesce(sum(ram_bytes),0),coalesce(sum(vram_bytes),0),coalesce(sum(scratch_bytes),0),coalesce(sum(gpu_units),0)
 INTO used_ram,used_vram,used_scratch,used_gpu FROM photo.resource_reservations WHERE node_id=p_node AND state='reserved';
 IF used_ram+p_ram>least(n.ram_budget_bytes,n.ram_available_bytes) OR used_vram+p_vram>least(n.vram_budget_bytes,n.vram_available_bytes)
 OR used_scratch+p_scratch>least(n.scratch_budget_bytes,n.scratch_available_bytes) OR NOT photo.capacity_policy_node_concurrency_available(n,t.resource_class) THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT coalesce((quotas->>'max_active_gpu_tasks')::integer,1) INTO quota_gpu FROM photo.tenants WHERE id=t.tenant_id;
 IF t.resource_class='gpu' AND (SELECT active_gpu_tasks>=quota_gpu FROM photo.tenant_usage WHERE tenant_id=t.tenant_id) THEN
 RETURN QUERY SELECT 'waiting_quota',NULL::jsonb; RETURN; END IF;
 -- Artifact locks are acquired after task, in UUID order, including params.
 IF jsonb_typeof(t.inputs)<>'array' OR jsonb_array_length(t.inputs)>512 THEN RAISE EXCEPTION 'INVALID_INPUTS'; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(input)<>'object' OR (input-'artifact_id'-'sha256')<>'{}' OR input->>'artifact_id' IS NULL OR input->>'sha256' IS NULL OR input->>'sha256' !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INVALID_INPUTS' USING ERRCODE='22023'; END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.resource_reservations WHERE tenant_id=t.tenant_id AND task_id=t.id AND state='reserved') THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs || jsonb_build_array(jsonb_build_object('artifact_id',t.params_artifact_id))) ORDER BY value->>'artifact_id' LOOP
  SELECT * INTO ai FROM photo.artifacts WHERE tenant_id=t.tenant_id AND id=(input->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND OR ai.state<>'ready' OR (ai.expires_at IS NOT NULL AND ai.expires_at<=clock_timestamp()) OR
   (input ? 'sha256' AND ai.sha256 IS DISTINCT FROM input->>'sha256') THEN RETURN QUERY SELECT 'input_unavailable',NULL::jsonb; RETURN; END IF;
  IF ai.id=t.params_artifact_id THEN params:=ai; END IF;
 END LOOP;
 envelope:=jsonb_build_object('schema_version',1,'event_id',p_event,'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,
 'attempt',t.attempt+1,'operation',t.operation,'operation_version',t.operation_version,'resource_class',t.resource_class,'correlation_id',p_correlation,
 'inputs',t.inputs,'params',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256),'model',model,'timeout_seconds',op.timeout_seconds,'traceparent',p_trace);
 IF octet_length(envelope::text)>65536 THEN RAISE EXCEPTION 'COMMAND_TOO_LARGE' USING ERRCODE='22023'; END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,state,expires_at,fencing_token,gpu_units)
 VALUES(t.tenant_id,t.id,p_node,p_scratch,p_ram,p_vram,'reserved',until_time,t.fencing_token,CASE WHEN t.resource_class='gpu' THEN 1 ELSE 0 END)
 ON CONFLICT(tenant_id,task_id) DO UPDATE SET node_id=excluded.node_id,scratch_bytes=excluded.scratch_bytes,ram_bytes=excluded.ram_bytes,vram_bytes=excluded.vram_bytes,
 state='reserved',expires_at=excluded.expires_at,fencing_token=excluded.fencing_token,gpu_units=excluded.gpu_units;
 UPDATE photo.resource_slots SET task_id=t.id,tenant_id=t.tenant_id,fencing_token=t.fencing_token,lease_until=until_time WHERE id=slot.id;
 IF t.resource_class='gpu' THEN UPDATE photo.tenant_usage SET active_gpu_tasks=active_gpu_tasks+1 WHERE tenant_id=t.tenant_id; END IF;
 UPDATE photo.tasks SET state='queued',dispatch_command=envelope,timeout_seconds=op.timeout_seconds,terminal_reason=NULL WHERE id=t.id;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,p_event,'photo.commands.v1',op.routing_key,envelope);
 RETURN QUERY SELECT 'queued',envelope;
END $function$
$preimage$ THEN RAISE EXCEPTION 'ARCHIVE5000_FUNCTION_PREIMAGE_2';END IF;
 SELECT to_jsonb(p)INTO old_meta FROM pg_proc p WHERE oid='photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure;
 EXECUTE $definition$CREATE OR REPLACE FUNCTION photo.reserve_dispatch_resources(p_task uuid, p_node text, p_ram bigint, p_vram bigint, p_scratch bigint, p_event uuid, p_correlation uuid, p_trace text)
 RETURNS TABLE(outcome text, command jsonb)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE n photo.resource_nodes%ROWTYPE; t photo.tasks%ROWTYPE; hint photo.tasks%ROWTYPE; r photo.runs%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; op photo.operation_registry%ROWTYPE; params photo.artifacts%ROWTYPE;
 used_ram numeric; used_vram numeric; used_scratch numeric; used_gpu integer; quota_gpu integer; until_time timestamptz;
 envelope jsonb; model jsonb; backend text; model_lock text; input jsonb; ai photo.artifacts%ROWTYPE;
 mask_dispatch_domain jsonb;
BEGIN
 IF p_task IS NULL OR p_event IS NULL OR p_correlation IS NULL OR p_node IS NULL OR p_trace IS NULL OR length(p_trace)>128
 OR p_ram IS NULL OR p_vram IS NULL OR p_scratch IS NULL OR p_ram<=0 OR p_vram<0 OR p_scratch<0 THEN
 RAISE EXCEPTION 'INVALID_RESERVATION' USING ERRCODE='22023'; END IF;
 SELECT * INTO hint FROM photo.tasks WHERE id=p_task;
 IF NOT FOUND THEN RETURN QUERY SELECT 'missing',NULL::jsonb; RETURN; END IF;

 mask_dispatch_domain:=photo.authored_execution_parent_domain(p_task);
 PERFORM photo.authored_mask_lock_execution_parent(p_task);
 IF photo.authored_execution_parent_domain(p_task) IS DISTINCT FROM mask_dispatch_domain THEN
  RAISE EXCEPTION 'AUTHORED_MASK_DISPATCH_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;
 SELECT * INTO n FROM photo.resource_nodes WHERE node_id=p_node FOR UPDATE SKIP LOCKED;
 IF FOUND AND photo.authored_execution_parent_domain(p_task) IS DISTINCT FROM mask_dispatch_domain THEN
  RAISE EXCEPTION 'AUTHORED_MASK_DISPATCH_DOMAIN_CHANGED' USING ERRCODE='40001';END IF;

 IF NOT FOUND OR NOT n.enabled OR NOT photo.capacity_policy_node_fresh(n) OR n.doctor_report_sha256 IS NULL THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 p_scratch:=photo.capacity_policy_scratch_reservation(n,p_scratch);
 IF p_scratch IS NULL THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE node_id=p_node AND resource_class=hint.resource_class AND task_id IS NULL ORDER BY slot_name LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=hint.tenant_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'TENANT_USAGE_MISSING'; END IF;
 SELECT * INTO r FROM photo.runs WHERE id=hint.run_id AND tenant_id=hint.tenant_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE id=p_task FOR UPDATE;
 IF t.state<>'ready' OR t.attempt>=6 OR t.tenant_id<>hint.tenant_id OR t.run_id<>hint.run_id OR t.resource_class<>slot.resource_class THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 IF t.cancel_requested OR r.cancel_requested OR r.status NOT IN ('queued','running') THEN
 RETURN QUERY SELECT 'cancelled',NULL::jsonb; RETURN; END IF;
 IF NOT r.graph_sealed THEN RETURN QUERY SELECT 'unsealed',NULL::jsonb; RETURN; END IF;
 SELECT * INTO op FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version;
 IF NOT FOUND OR op.resource_class<>t.resource_class THEN RAISE EXCEPTION 'OPERATION_MISMATCH' USING ERRCODE='22023'; END IF;
 backend:=t.execution_profile->>'backend';
 IF backend IS NULL OR NOT op.backends ? backend THEN RETURN QUERY SELECT 'unsupported_backend',NULL::jsonb; RETURN; END IF;
 model:=NULL;
 IF op.model_id IS NOT NULL THEN
  model_lock:=t.execution_profile->>'model_lock_id';
  -- Identified native model profiles pin model_lock_sha. Never rewrite old tasks.
  IF t.operation IN('mask.person','face.detect','face.landmarks','face.parse','scene.parse','depth.predict') AND t.operation_version='1'
   AND t.execution_profile->>'renderer_version'='v2.2' THEN
   IF model_lock IS NOT NULL AND model_lock IS DISTINCT FROM t.execution_profile->>'model_lock_sha' THEN
    RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN;
   END IF;
   model_lock:=t.execution_profile->>'model_lock_sha';
  END IF;

  IF model_lock IS NULL OR model_lock !~ '^[0-9a-f]{64}$' THEN RETURN QUERY SELECT 'unqualified_model',NULL::jsonb; RETURN; END IF;
  model:=jsonb_build_object('id',op.model_id,'lock_id',model_lock);
 END IF;
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.node_id=p_node AND w.enabled
 AND photo.authored_render_worker_pair_ready(t.id,w.worker_id)
 AND w.db_principal IS NOT NULL AND w.resource_classes ? t.resource_class AND w.backends ? backend AND (w.qualified_operations->t.operation) ? backend
   AND (t.operation<>'series.balance' OR
    t.execution_profile->>'device_profile_sha' IS NULL OR
    photo.v23_series_worker_device_matches(t.tenant_id,w.worker_id,t.execution_profile))
 AND photo.v31_signer_worker_matches(t.tenant_id,w.worker_id,t.operation,t.execution_profile) AND photo.capacity_policy_worker_fresh(w) AND (op.model_id IS NOT NULL AND w.lock_id=model_lock OR
  op.model_id IS NULL AND (t.operation<>'series.balance' OR
   t.execution_profile->>'model_lock_id' IS NULL OR
   w.lock_id=t.execution_profile->>'model_lock_id'))) THEN
 RETURN QUERY SELECT 'waiting_worker',NULL::jsonb; RETURN; END IF;
 SELECT coalesce(sum(ram_bytes),0),coalesce(sum(vram_bytes),0),coalesce(sum(scratch_bytes),0),coalesce(sum(gpu_units),0)
 INTO used_ram,used_vram,used_scratch,used_gpu FROM photo.resource_reservations WHERE node_id=p_node AND state='reserved';
 IF used_ram+p_ram>least(n.ram_budget_bytes,n.ram_available_bytes) OR used_vram+p_vram>least(n.vram_budget_bytes,n.vram_available_bytes)
 OR used_scratch+p_scratch>least(n.scratch_budget_bytes,n.scratch_available_bytes) OR NOT photo.capacity_policy_node_concurrency_available(n,t.resource_class) THEN
 RETURN QUERY SELECT 'waiting_resource',NULL::jsonb; RETURN; END IF;
 SELECT coalesce((quotas->>'max_active_gpu_tasks')::integer,1) INTO quota_gpu FROM photo.tenants WHERE id=t.tenant_id;
 IF t.resource_class='gpu' AND (SELECT active_gpu_tasks>=quota_gpu FROM photo.tenant_usage WHERE tenant_id=t.tenant_id) THEN
 RETURN QUERY SELECT 'waiting_quota',NULL::jsonb; RETURN; END IF;
 -- Artifact locks are acquired after task, in UUID order, including params.
 IF jsonb_typeof(t.inputs)<>'array' OR jsonb_array_length(t.inputs)>(CASE WHEN t.operation='archive.build' AND t.operation_version='1' AND t.resource_class='archive' THEN 5000 ELSE 512 END) THEN RAISE EXCEPTION 'INVALID_INPUTS'; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs) LOOP
  IF jsonb_typeof(input)<>'object' OR (input-'artifact_id'-'sha256')<>'{}' OR input->>'artifact_id' IS NULL OR input->>'sha256' IS NULL OR input->>'sha256' !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'INVALID_INPUTS' USING ERRCODE='22023'; END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.resource_reservations WHERE tenant_id=t.tenant_id AND task_id=t.id AND state='reserved') THEN
 RETURN QUERY SELECT 'stale',NULL::jsonb; RETURN; END IF;
 FOR input IN SELECT value FROM jsonb_array_elements(t.inputs || jsonb_build_array(jsonb_build_object('artifact_id',t.params_artifact_id))) ORDER BY value->>'artifact_id' LOOP
  SELECT * INTO ai FROM photo.artifacts WHERE tenant_id=t.tenant_id AND id=(input->>'artifact_id')::uuid FOR SHARE;
  IF NOT FOUND OR ai.state<>'ready' OR (ai.expires_at IS NOT NULL AND ai.expires_at<=clock_timestamp()) OR
   (input ? 'sha256' AND ai.sha256 IS DISTINCT FROM input->>'sha256') THEN RETURN QUERY SELECT 'input_unavailable',NULL::jsonb; RETURN; END IF;
  IF ai.id=t.params_artifact_id THEN params:=ai; END IF;
 END LOOP;
 envelope:=jsonb_build_object('schema_version',1,'event_id',p_event,'tenant_id',t.tenant_id,'run_id',t.run_id,'task_id',t.id,
 'attempt',t.attempt+1,'operation',t.operation,'operation_version',t.operation_version,'resource_class',t.resource_class,'correlation_id',p_correlation,
 'inputs',t.inputs,'params',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256),'model',model,'timeout_seconds',op.timeout_seconds,'traceparent',p_trace);
 IF octet_length(envelope::text)>(CASE WHEN t.operation='archive.build' AND t.operation_version='1' AND t.resource_class='archive' THEN 1048576 ELSE 65536 END) THEN RAISE EXCEPTION 'COMMAND_TOO_LARGE' USING ERRCODE='22023'; END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 INSERT INTO photo.resource_reservations(tenant_id,task_id,node_id,scratch_bytes,ram_bytes,vram_bytes,state,expires_at,fencing_token,gpu_units)
 VALUES(t.tenant_id,t.id,p_node,p_scratch,p_ram,p_vram,'reserved',until_time,t.fencing_token,CASE WHEN t.resource_class='gpu' THEN 1 ELSE 0 END)
 ON CONFLICT(tenant_id,task_id) DO UPDATE SET node_id=excluded.node_id,scratch_bytes=excluded.scratch_bytes,ram_bytes=excluded.ram_bytes,vram_bytes=excluded.vram_bytes,
 state='reserved',expires_at=excluded.expires_at,fencing_token=excluded.fencing_token,gpu_units=excluded.gpu_units;
 UPDATE photo.resource_slots SET task_id=t.id,tenant_id=t.tenant_id,fencing_token=t.fencing_token,lease_until=until_time WHERE id=slot.id;
 IF t.resource_class='gpu' THEN UPDATE photo.tenant_usage SET active_gpu_tasks=active_gpu_tasks+1 WHERE tenant_id=t.tenant_id; END IF;
 UPDATE photo.tasks SET state='queued',dispatch_command=envelope,timeout_seconds=op.timeout_seconds,terminal_reason=NULL WHERE id=t.id;
 INSERT INTO photo.outbox(tenant_id,event_id,destination,routing_key,payload) VALUES(t.tenant_id,p_event,'photo.commands.v1',op.routing_key,envelope);
 RETURN QUERY SELECT 'queued',envelope;
END $function$
$definition$;
 SELECT to_jsonb(p)INTO new_meta FROM pg_proc p WHERE oid='photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)'::regprocedure;
 IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' THEN RAISE EXCEPTION 'ARCHIVE5000_METADATA_DRIFT_2';END IF;
 END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $migration$ DECLARE old_meta jsonb;new_meta jsonb;BEGIN
 IF pg_get_functiondef('photo.claim_task_reserved(uuid,integer,uuid,jsonb)'::regprocedure) IS DISTINCT FROM $preimage$CREATE OR REPLACE FUNCTION photo.claim_task_reserved(p_task uuid, p_attempt integer, p_worker uuid, p_command jsonb)
 RETURNS TABLE(outcome text, fence bigint, lease_until timestamp with time zone, deadline timestamp with time zone, context jsonb)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE t photo.tasks%ROWTYPE; slot photo.resource_slots%ROWTYPE; r photo.runs%ROWTYPE; until_time timestamptz; end_time timestamptz;
BEGIN
 IF p_attempt IS NULL OR p_attempt NOT BETWEEN 1 AND 6 OR p_worker IS NULL OR p_command IS NULL OR octet_length(p_command::text)>65536 THEN
  RAISE EXCEPTION 'INVALID_COMMAND' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND tenant_id=t.tenant_id
 AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 IF t.id IS NULL THEN RETURN QUERY SELECT 'missing',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=t.id AND tenant_id=t.tenant_id;
 -- Terminal duplicates still require a registered identity, but no longer own a slot.
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker
    AND w.db_principal=session_user AND w.enabled AND w.resource_classes ? t.resource_class) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.state IN ('succeeded','failed','cancelled','skipped') THEN
  RETURN QUERY SELECT 'terminal',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.dispatch_command IS NULL OR t.dispatch_command<>p_command THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE id=t.run_id AND tenant_id=t.tenant_id;
 IF t.cancel_requested OR r.cancel_requested THEN
  RETURN QUERY SELECT 'cancelled',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.state='running' THEN RETURN QUERY SELECT 'busy',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN; END IF;
 IF t.state<>'queued' OR p_attempt<>t.attempt+1 OR (p_command->>'attempt')::integer<>p_attempt THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF slot.id IS NULL OR slot.lease_until IS NULL OR slot.fencing_token IS DISTINCT FROM t.fencing_token OR slot.lease_until<=clock_timestamp() OR
 NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') OR
 NOT EXISTS(SELECT FROM photo.resource_reservations rr WHERE rr.tenant_id=t.tenant_id AND rr.task_id=t.id AND rr.node_id=slot.node_id
 AND rr.state='reserved' AND rr.expires_at>clock_timestamp()) THEN
  RETURN QUERY SELECT 'unavailable',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 end_time:=clock_timestamp()+make_interval(secs=>t.timeout_seconds);
 until_time:=least(clock_timestamp()+make_interval(secs=>photo.capacity_policy_task_lease_seconds(t.tenant_id,p_worker,t.timeout_seconds)),end_time);
 UPDATE photo.tasks SET state='running',attempt=p_attempt,fencing_token=t.fencing_token+1,worker_id=p_worker,
 lease_until=until_time,deadline=end_time,progress='{}' WHERE id=t.id;
 UPDATE photo.resource_slots SET fencing_token=t.fencing_token+1,lease_until=until_time WHERE id=slot.id;
 UPDATE photo.resource_reservations SET fencing_token=t.fencing_token+1,expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile)
 VALUES(t.tenant_id,t.id,p_attempt,t.fencing_token+1,p_worker,clock_timestamp(),t.execution_profile->>'backend',t.execution_profile);
 RETURN QUERY SELECT 'claimed',t.fencing_token+1,until_time,end_time,
 jsonb_build_object('command',t.dispatch_command,'execution_profile',t.execution_profile,'input_digest',t.input_digest);
END $function$
$preimage$ THEN RAISE EXCEPTION 'ARCHIVE5000_FUNCTION_PREIMAGE_3';END IF;
 SELECT to_jsonb(p)INTO old_meta FROM pg_proc p WHERE oid='photo.claim_task_reserved(uuid,integer,uuid,jsonb)'::regprocedure;
 EXECUTE $definition$CREATE OR REPLACE FUNCTION photo.claim_task_reserved(p_task uuid, p_attempt integer, p_worker uuid, p_command jsonb)
 RETURNS TABLE(outcome text, fence bigint, lease_until timestamp with time zone, deadline timestamp with time zone, context jsonb)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE t photo.tasks%ROWTYPE; slot photo.resource_slots%ROWTYPE; r photo.runs%ROWTYPE; until_time timestamptz; end_time timestamptz;
BEGIN
 IF p_attempt IS NULL OR p_attempt NOT BETWEEN 1 AND 6 OR p_worker IS NULL OR p_command IS NULL OR octet_length(p_command::text)>(CASE WHEN p_command->>'operation'='archive.build' AND p_command->>'operation_version'='1' AND p_command->>'resource_class'='archive' THEN 1048576 ELSE 65536 END) THEN
  RAISE EXCEPTION 'INVALID_COMMAND' USING ERRCODE='22023'; END IF;
 t:=photo.lock_task(p_task);
 PERFORM id FROM photo.worker_registrations WHERE worker_id=p_worker AND tenant_id=t.tenant_id
 AND db_principal=session_user AND enabled ORDER BY id FOR UPDATE;
 IF t.id IS NULL THEN RETURN QUERY SELECT 'missing',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE task_id=t.id AND tenant_id=t.tenant_id;
 -- Terminal duplicates still require a registered identity, but no longer own a slot.
 IF NOT EXISTS(SELECT FROM photo.worker_registrations w WHERE w.tenant_id=t.tenant_id AND w.worker_id=p_worker
    AND w.db_principal=session_user AND w.enabled AND w.resource_classes ? t.resource_class) THEN
  RETURN QUERY SELECT 'unauthorized',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.state IN ('succeeded','failed','cancelled','skipped') THEN
  RETURN QUERY SELECT 'terminal',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.dispatch_command IS NULL OR t.dispatch_command<>p_command THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 SELECT * INTO r FROM photo.runs WHERE id=t.run_id AND tenant_id=t.tenant_id;
 IF t.cancel_requested OR r.cancel_requested THEN
  RETURN QUERY SELECT 'cancelled',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF t.state='running' THEN RETURN QUERY SELECT 'busy',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN; END IF;
 IF t.state<>'queued' OR p_attempt<>t.attempt+1 OR (p_command->>'attempt')::integer<>p_attempt THEN
  RETURN QUERY SELECT 'stale',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 IF slot.id IS NULL OR slot.lease_until IS NULL OR slot.fencing_token IS DISTINCT FROM t.fencing_token OR slot.lease_until<=clock_timestamp() OR
 NOT photo.worker_authorized(t.tenant_id,p_worker,t.resource_class,slot.node_id,t.operation,t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') OR
 NOT EXISTS(SELECT FROM photo.resource_reservations rr WHERE rr.tenant_id=t.tenant_id AND rr.task_id=t.id AND rr.node_id=slot.node_id
 AND rr.state='reserved' AND rr.expires_at>clock_timestamp()) THEN
  RETURN QUERY SELECT 'unavailable',NULL::bigint,NULL::timestamptz,NULL::timestamptz,NULL::jsonb; RETURN;
 END IF;
 end_time:=clock_timestamp()+make_interval(secs=>t.timeout_seconds);
 until_time:=least(clock_timestamp()+make_interval(secs=>photo.capacity_policy_task_lease_seconds(t.tenant_id,p_worker,t.timeout_seconds)),end_time);
 UPDATE photo.tasks SET state='running',attempt=p_attempt,fencing_token=t.fencing_token+1,worker_id=p_worker,
 lease_until=until_time,deadline=end_time,progress='{}' WHERE id=t.id;
 UPDATE photo.resource_slots SET fencing_token=t.fencing_token+1,lease_until=until_time WHERE id=slot.id;
 UPDATE photo.resource_reservations SET fencing_token=t.fencing_token+1,expires_at=until_time WHERE tenant_id=t.tenant_id AND task_id=t.id;
 UPDATE photo.worker_registrations SET last_seen=clock_timestamp() WHERE tenant_id=t.tenant_id AND worker_id=p_worker;
 INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile)
 VALUES(t.tenant_id,t.id,p_attempt,t.fencing_token+1,p_worker,clock_timestamp(),t.execution_profile->>'backend',t.execution_profile);
 RETURN QUERY SELECT 'claimed',t.fencing_token+1,until_time,end_time,
 jsonb_build_object('command',t.dispatch_command,'execution_profile',t.execution_profile,'input_digest',t.input_digest);
END $function$
$definition$;
 SELECT to_jsonb(p)INTO new_meta FROM pg_proc p WHERE oid='photo.claim_task_reserved(uuid,integer,uuid,jsonb)'::regprocedure;
 IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' THEN RAISE EXCEPTION 'ARCHIVE5000_METADATA_DRIFT_3';END IF;
 END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $migration$ DECLARE old_meta jsonb;new_meta jsonb;BEGIN
 IF pg_get_functiondef('photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text)'::regprocedure) IS DISTINCT FROM $preimage$CREATE OR REPLACE FUNCTION photo.reserve_backend_document(p_tenant uuid, p_run uuid, p_owner uuid, p_purpose text, p_sha text, p_bytes bigint, p_bucket text)
 RETURNS TABLE(outcome text, artifact_id uuid, token uuid, ready boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE; capacity numeric; used numeric; aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid();
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL OR p_purpose IS NULL OR p_purpose NOT IN ('task_params','review_selection')
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN ('queued','running','waiting_review') THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 IF (p_purpose='review_selection' AND NOT EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.id=p_owner AND v.kind='defects' AND v.state='decided'))
 OR (p_purpose='task_params' AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_owner AND e.state='open' AND e.kind LIKE 'review.defects/%') AND NOT photo.download_export_document_owner(p_tenant,p_run,p_owner) AND NOT (r.kind='ingest' AND NOT r.graph_sealed AND EXISTS(SELECT FROM photo.ingest_node_plans n WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.id=p_owner AND n.params_sha=p_sha))) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=d.object_version) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN; END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket)
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $function$
$preimage$ THEN RAISE EXCEPTION 'ARCHIVE5000_FUNCTION_PREIMAGE_4';END IF;
 SELECT to_jsonb(p)INTO old_meta FROM pg_proc p WHERE oid='photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text)'::regprocedure;
 EXECUTE $definition$CREATE OR REPLACE FUNCTION photo.reserve_backend_document(p_tenant uuid, p_run uuid, p_owner uuid, p_purpose text, p_sha text, p_bytes bigint, p_bucket text)
 RETURNS TABLE(outcome text, artifact_id uuid, token uuid, ready boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'photo'
AS $function$
DECLARE d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE; capacity numeric; used numeric; aid uuid:=gen_random_uuid(); fence uuid:=gen_random_uuid(); archive_id uuid;
BEGIN
 IF p_tenant IS NULL OR p_run IS NULL OR p_owner IS NULL OR p_purpose IS NULL OR p_purpose NOT IN ('task_params','review_selection')
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 8388608
 OR p_bucket IS NULL OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023'; END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN ('queued','running','waiting_review') THEN
  RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 IF p_bytes>1048576 AND NOT(p_purpose='task_params' AND r.kind='download' AND NOT r.graph_sealed AND EXISTS(SELECT FROM photo.download_archive_plans ap WHERE ap.tenant_id=p_tenant AND ap.run_id=p_run AND ap.id=p_owner AND ap.params_sha=p_sha AND jsonb_array_length(ap.inputs) BETWEEN 1 AND 5000 AND octet_length(ap.params::text)<=8388608)) THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023';END IF;
 IF p_bytes>1048576 THEN archive_id:=p_owner;END IF;
 IF (p_purpose='review_selection' AND NOT EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.id=p_owner AND v.kind='defects' AND v.state='decided'))
 OR (p_purpose='task_params' AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_owner AND e.state='open' AND e.kind LIKE 'review.defects/%') AND NOT photo.download_export_document_owner(p_tenant,p_run,p_owner) AND NOT (r.kind='ingest' AND NOT r.graph_sealed AND EXISTS(SELECT FROM photo.ingest_node_plans n WHERE n.tenant_id=p_tenant AND n.run_id=p_run AND n.id=p_owner AND n.params_sha=p_sha))) THEN
  RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket OR d.archive_plan_id IS DISTINCT FROM archive_id THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN; END IF;
  IF d.state='ready' THEN
   IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=d.object_version) THEN
    RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN; END IF;
   RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;
  END IF;
  IF d.state<>'pending' THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;
  RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity OR used+p_bytes>9007199254740991 THEN
  RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN; END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,archive_plan_id)
 VALUES(p_tenant,p_run,p_owner,p_purpose,aid,fence,p_sha,p_bytes,p_bucket,archive_id);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;
 RETURN QUERY SELECT 'created',aid,fence,false;
END $function$
$definition$;
 SELECT to_jsonb(p)INTO new_meta FROM pg_proc p WHERE oid='photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text)'::regprocedure;
 IF old_meta-'prosrc' IS DISTINCT FROM new_meta-'prosrc' THEN RAISE EXCEPTION 'ARCHIVE5000_METADATA_DRIFT_4';END IF;
 END $migration$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE photo.operation_output_policies IN SHARE ROW EXCLUSIVE MODE;
 IF (SELECT jsonb_agg(jsonb_build_array(role,media_types,max_bytes,max_count)ORDER BY role)FROM photo.operation_output_policies WHERE operation='archive.build'AND version='1') IS DISTINCT FROM '[["archive",["application/zip"],10758946816,1],["archive_manifest",["application/json"],1048576,1]]'::jsonb THEN RAISE EXCEPTION 'ARCHIVE5000_POLICY_PREIMAGE';END IF;
 UPDATE photo.operation_output_policies SET max_bytes=10766286848 WHERE operation='archive.build'AND version='1'AND role='archive';
 UPDATE photo.operation_output_policies SET max_bytes=8388608 WHERE operation='archive.build'AND version='1'AND role='archive_manifest';
 END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'FORWARD_ONLY';END $$;
-- +goose StatementEnd
