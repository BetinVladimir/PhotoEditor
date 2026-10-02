-- +goose Up
CREATE TABLE photo.download_archive_plans(
 id uuid PRIMARY KEY,tenant_id uuid NOT NULL,run_id uuid NOT NULL,volume_no integer NOT NULL CHECK(volume_no BETWEEN 1 AND 5000),
 node_key text NOT NULL,operation text NOT NULL DEFAULT 'archive.build' CHECK(operation='archive.build'),
 inputs jsonb NOT NULL CHECK(jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) BETWEEN 1 AND 5000),
 producer_tasks jsonb NOT NULL CHECK(jsonb_typeof(producer_tasks)='array' AND jsonb_array_length(producer_tasks)=jsonb_array_length(inputs)),
 params_sha text NOT NULL CHECK(params_sha ~ '^[0-9a-f]{64}$'),params jsonb NOT NULL CHECK(jsonb_typeof(params)='object'),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,id),UNIQUE(tenant_id,run_id,volume_no),UNIQUE(tenant_id,run_id,node_key),FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id)
);
ALTER TABLE photo.download_archive_plans ENABLE ROW LEVEL SECURITY;ALTER TABLE photo.download_archive_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.download_archive_plans TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.download_archive_plans TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.download_export_document_owner(p_tenant uuid,p_run uuid,p_owner uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='download' AND NOT r.graph_sealed
 AND NOT r.cancel_requested AND r.status IN ('queued','running') AND (EXISTS(SELECT FROM photo.download_export_plans p WHERE p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.id=p_owner)
 OR EXISTS(SELECT FROM photo.download_archive_plans p WHERE p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.id=p_owner)))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_export_document_owner(uuid,uuid,uuid) OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.download_archive_context(p_tenant uuid,p_run uuid)
RETURNS TABLE(snapshot_id uuid,snapshot_digest text,volume_no integer,delivery_kind text,ordinal bigint,artifact_id uuid,sha256 text,bytes bigint,entry_name text,producer_task_id uuid)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT s.id,s.digest,i.volume_no,i.delivery_kind,i.ordinal,i.artifact_id,i.sha256,i.bytes,i.entry_name,p.id
 FROM photo.download_build_state b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id
 JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id
 JOIN photo.download_job_items i ON i.tenant_id=j.tenant_id AND i.download_job_id=j.id
 JOIN photo.download_export_plans p ON p.tenant_id=r.tenant_id AND p.run_id=r.id AND p.ordinal=i.ordinal
 JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id AND t.state='succeeded' AND t.operation='export.encode'
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 AND a.bytes=i.bytes AND a.state='ready' AND a.created_by_task_id=t.id AND a.output_role='encoded_image'
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.phase='packaging' AND NOT r.graph_sealed AND r.status='running' AND NOT r.cancel_requested
 AND j.status='building' AND NOT j.cancel_requested AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY i.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_archive_context(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_archive_context(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_archive_context(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.register_download_archive_plan(p_tenant uuid,p_run uuid,p_id uuid,p_volume integer,p_key text,p_inputs jsonb,p_producers jsonb,p_params_sha text,p_params jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;b photo.download_build_state%ROWTYPE;old photo.download_archive_plans%ROWTYPE;expected_inputs jsonb;expected_producers jsonb;expected_params jsonb;
BEGIN
 IF p_id IS NULL OR p_volume NOT BETWEEN 1 AND 5000 OR p_key IS DISTINCT FROM 'archive.build/'||lpad(p_volume::text,4,'0')
 OR jsonb_typeof(p_inputs)<>'array' OR jsonb_array_length(p_inputs) NOT BETWEEN 1 AND 5000 OR jsonb_typeof(p_producers)<>'array' OR jsonb_array_length(p_producers)<>jsonb_array_length(p_inputs)
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
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.materialize_download_archive_task(p_tenant uuid,p_run uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;n photo.download_archive_plans%ROWTYPE;old photo.tasks%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;producer text;entry record;value numeric;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object' OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO n FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF r.id IS NULL OR n.id IS NULL OR r.graph_sealed OR r.cancel_requested OR r.status<>'running' OR p_task->>'node_key'<>n.node_key OR p_task->>'operation'<>'archive.build'
 OR p_task->>'operation_version'<>'1' OR p_task->'inputs'<>n.inputs OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
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
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_download_archive_task(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_download_archive_task(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_download_archive_task(uuid,uuid,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.seal_download_archives(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;b photo.download_build_state%ROWTYPE;zip_count bigint;until_time timestamptz;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;SELECT * INTO b FROM photo.download_build_state WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF r.id IS NULL OR j.id IS NULL OR b.run_id IS NULL OR b.phase<>'packaging' OR r.graph_sealed OR r.cancel_requested OR j.cancel_requested THEN RETURN false;END IF;
 SELECT count(DISTINCT volume_no) INTO zip_count FROM photo.download_job_items WHERE tenant_id=p_tenant AND download_job_id=j.id AND delivery_kind='zip';
 IF (SELECT count(*) FROM photo.download_archive_plans WHERE tenant_id=p_tenant AND run_id=p_run)<>zip_count
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='archive.build')<>zip_count THEN RETURN false;END IF;
 IF zip_count=0 THEN
  until_time:=clock_timestamp()+interval '24 hours';
  UPDATE photo.artifacts SET expires_at=until_time WHERE tenant_id=p_tenant AND id IN (SELECT artifact_id FROM photo.download_volumes WHERE tenant_id=p_tenant AND download_job_id=j.id);
  UPDATE photo.download_build_state SET phase='archiving',updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
  UPDATE photo.runs SET graph_sealed=true,status='completed',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run;
  UPDATE photo.download_jobs SET status='ready',expires_at=until_time WHERE tenant_id=p_tenant AND id=j.id;RETURN true;
 END IF;
 IF EXISTS(SELECT FROM photo.download_archive_plans p LEFT JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND (t.id IS NULL OR t.state<>'blocked' OR t.inputs<>p.inputs)) THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready',next_attempt_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND operation='archive.build' AND state='blocked';
 UPDATE photo.download_build_state SET phase='archiving',updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_download_archives(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_download_archives(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_download_archives(uuid,uuid) TO photo_scheduler;

-- Packaging is claimable only after its rows exist; archiving awaits workers.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_download_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;token uuid:=gen_random_uuid();until_time timestamptz;
BEGIN
 SELECT rr.* INTO r FROM photo.runs rr JOIN photo.download_jobs j ON j.tenant_id=rr.tenant_id AND j.run_id=rr.id JOIN photo.download_build_state b ON b.tenant_id=rr.tenant_id AND b.run_id=rr.id
 WHERE rr.kind='download' AND rr.status IN ('queued','running') AND NOT rr.cancel_requested AND NOT j.cancel_requested AND rr.next_download_build_at<=clock_timestamp() AND (rr.download_build_until IS NULL OR rr.download_build_until<=clock_timestamp())
 AND ((b.phase='planning' AND NOT rr.graph_sealed AND j.status IN ('queued','preparing')) OR (b.phase='encoding' AND rr.graph_sealed AND j.status='building' AND NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=rr.tenant_id AND t.run_id=rr.id AND t.operation='export.encode' AND t.state NOT IN ('succeeded','failed','cancelled','skipped')))
 OR (b.phase='packaging' AND NOT rr.graph_sealed AND j.status='building'))
 ORDER BY rr.next_download_build_at,rr.id LIMIT 1 FOR UPDATE OF rr SKIP LOCKED;IF NOT FOUND THEN RETURN;END IF;
 until_time:=clock_timestamp()+interval '60 seconds';UPDATE photo.runs SET download_build_token=token,download_build_until=until_time WHERE id=r.id;
 UPDATE photo.download_jobs SET status='preparing' WHERE tenant_id=r.tenant_id AND run_id=r.id AND status='queued';RETURN QUERY SELECT r.tenant_id,r.id,token,until_time;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_download_build() OWNER TO photo_fn_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.renew_download_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs r SET download_build_until=clock_timestamp()+interval '60 seconds' FROM photo.download_build_state b
 WHERE r.id=p_run AND r.kind='download' AND r.status IN ('queued','running') AND NOT r.cancel_requested AND r.download_build_token=p_token
 AND r.download_build_until>clock_timestamp() AND b.tenant_id=r.tenant_id AND b.run_id=r.id AND b.phase IN ('planning','encoding','packaging');RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_download_build(uuid,uuid) OWNER TO photo_fn_owner;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
