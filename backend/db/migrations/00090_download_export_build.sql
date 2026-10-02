-- +goose Up
ALTER TABLE photo.runs ADD COLUMN download_build_token uuid;
ALTER TABLE photo.runs ADD COLUMN download_build_until timestamptz;
ALTER TABLE photo.runs ADD COLUMN next_download_build_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE photo.runs ADD CONSTRAINT download_build_lease_pair CHECK((download_build_token IS NULL)=(download_build_until IS NULL));
CREATE INDEX download_build_pending ON photo.runs(next_download_build_at,id)
 WHERE kind='download' AND status IN ('queued','running') AND NOT cancel_requested AND NOT graph_sealed;

CREATE TABLE photo.download_export_plans (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, run_id uuid NOT NULL,
 ordinal bigint NOT NULL CHECK(ordinal BETWEEN 0 AND 4999), node_key text NOT NULL,
 operation text NOT NULL DEFAULT 'export.encode' CHECK(operation='export.encode'),
 inputs jsonb NOT NULL CHECK(jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) BETWEEN 2 AND 3),
 params_sha text NOT NULL CHECK(params_sha ~ '^[0-9a-f]{64}$'),
 params jsonb NOT NULL CHECK(jsonb_typeof(params)='object'), created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,run_id,ordinal), UNIQUE(tenant_id,run_id,node_key),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id)
);
ALTER TABLE photo.download_export_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.download_export_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.download_export_plans TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT ON photo.download_export_plans TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_download_build()
RETURNS TABLE(tenant_id uuid,run_id uuid,lease_token uuid,lease_until timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; token uuid:=gen_random_uuid(); until_time timestamptz;
BEGIN
 SELECT rr.* INTO r FROM photo.runs rr JOIN photo.download_jobs j ON j.tenant_id=rr.tenant_id AND j.run_id=rr.id
 WHERE rr.kind='download' AND rr.status IN ('queued','running') AND NOT rr.cancel_requested AND NOT rr.graph_sealed
 AND j.status IN ('queued','preparing') AND NOT j.cancel_requested AND rr.next_download_build_at<=clock_timestamp()
 AND (rr.download_build_until IS NULL OR rr.download_build_until<=clock_timestamp())
 ORDER BY rr.next_download_build_at,rr.id LIMIT 1 FOR UPDATE OF rr SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 until_time:=clock_timestamp()+interval '60 seconds';
 UPDATE photo.runs SET download_build_token=token,download_build_until=until_time WHERE id=r.id;
 UPDATE photo.download_jobs SET status='preparing' WHERE tenant_id=r.tenant_id AND run_id=r.id AND status='queued';
 RETURN QUERY SELECT r.tenant_id,r.id,token,until_time;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_download_build() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.claim_download_build() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.claim_download_build() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.renew_download_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs SET download_build_until=clock_timestamp()+interval '60 seconds'
 WHERE id=p_run AND kind='download' AND status IN ('queued','running') AND NOT cancel_requested AND NOT graph_sealed
 AND download_build_token=p_token AND download_build_until>clock_timestamp();
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.renew_download_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.renew_download_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.renew_download_build(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.release_download_build(p_run uuid,p_token uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_token IS NULL THEN RETURN false;END IF;
 UPDATE photo.runs SET download_build_token=NULL,download_build_until=NULL,next_download_build_at=clock_timestamp()+interval '5 seconds'
 WHERE id=p_run AND kind='download' AND download_build_token=p_token;
 RETURN FOUND;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.release_download_build(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.release_download_build(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.release_download_build(uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.register_download_export_plan(p_tenant uuid,p_run uuid,p_id uuid,p_ordinal bigint,p_key text,p_inputs jsonb,p_params_sha text,p_params jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; old photo.download_export_plans%ROWTYPE; expected jsonb; entry jsonb;
BEGIN
 IF p_id IS NULL OR p_ordinal IS NULL OR p_ordinal NOT BETWEEN 0 AND 4999 OR p_key IS DISTINCT FROM 'export.encode/'||lpad(p_ordinal::text,16,'0')
 OR p_inputs IS NULL OR jsonb_typeof(p_inputs)<>'array' OR jsonb_array_length(p_inputs) NOT BETWEEN 2 AND 3
 OR p_params_sha IS NULL OR p_params_sha !~ '^[0-9a-f]{64}$' OR p_params IS NULL OR jsonb_typeof(p_params)<>'object'
 OR octet_length(p_params::text)>1048576 THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'download' OR r.status NOT IN ('queued','running') OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256) ORDER BY x.pos) INTO expected
 FROM jsonb_array_elements(p_inputs) WITH ORDINALITY x(value,pos)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(x.value->>'artifact_id')::uuid AND a.sha256=x.value->>'sha256'
  AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF expected IS DISTINCT FROM p_inputs OR p_params->'image' IS DISTINCT FROM p_inputs->0 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_inputs) LOOP
  IF (entry-'artifact_id'-'sha256')<>'{}' OR entry->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 SELECT * INTO old FROM photo.download_export_plans WHERE tenant_id=p_tenant AND run_id=p_run AND ordinal=p_ordinal;
 IF FOUND THEN RETURN old.id=p_id AND old.node_key=p_key AND old.inputs=p_inputs AND old.params_sha=p_params_sha AND old.params=p_params;END IF;
 IF EXISTS(SELECT FROM photo.download_export_plans WHERE id=p_id) THEN RETURN false;END IF;
 INSERT INTO photo.download_export_plans(id,tenant_id,run_id,ordinal,node_key,inputs,params_sha,params)
 VALUES(p_id,p_tenant,p_run,p_ordinal,p_key,p_inputs,p_params_sha,p_params);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_download_export_plan(uuid,uuid,uuid,bigint,text,jsonb,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_download_export_plan(uuid,uuid,uuid,bigint,text,jsonb,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_download_export_plan(uuid,uuid,uuid,bigint,text,jsonb,text,jsonb) TO photo_scheduler;

-- Permit task parameter documents for a registered, still-unsealed export plan.
-- Existing review and ingest owner scopes remain unchanged.
-- +goose StatementBegin
CREATE FUNCTION photo.download_export_document_owner(p_tenant uuid,p_run uuid,p_owner uuid) RETURNS boolean
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.download_export_plans p JOIN photo.runs r ON r.tenant_id=p.tenant_id AND r.id=p.run_id
 WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND p.id=p_owner AND r.kind='download' AND NOT r.graph_sealed
 AND NOT r.cancel_requested AND r.status IN ('queued','running'))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_export_document_owner(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_export_document_owner(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_export_document_owner(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.materialize_download_export_task(p_tenant uuid,p_run uuid,p_task jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;n photo.download_export_plans%ROWTYPE;old photo.tasks%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;v numeric;entry record;
BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object' OR octet_length(p_task::text)>1048576
 OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'download' OR r.status NOT IN ('queued','running') OR r.cancel_requested OR r.graph_sealed THEN RETURN false;END IF;
 SELECT * INTO n FROM photo.download_export_plans WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF NOT FOUND OR p_task->>'node_key' IS DISTINCT FROM n.node_key OR p_task->>'operation' IS DISTINCT FROM n.operation
 OR p_task->>'operation_version' IS DISTINCT FROM '1' OR p_task->'inputs' IS DISTINCT FROM n.inputs
 OR p_task->>'input_digest' IS NULL OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry op WHERE op.operation='export.encode' AND op.version='1'
 AND op.resource_class=p_task->>'resource_class' AND op.timeout_seconds=(p_task->>'timeout_seconds')::integer) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;
 IF NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id
 WHERE a.tenant_id=p_tenant AND a.id=params_id AND a.state='ready' AND a.sha256=n.params_sha AND a.sha256=p_task->'params'->>'sha256'
 AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements(n.inputs) x LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant
  AND a.id=(x->>'artifact_id')::uuid AND a.sha256=x->>'sha256' AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE a.id IS NULL) THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';
 IF profile->>'backend'<>'cpu' OR profile->>'precision'<>'float32' OR jsonb_typeof(estimate)<>'object'
 OR (SELECT count(*) FROM jsonb_object_keys(estimate))<>5 OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count']) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_each(estimate) LOOP
  IF jsonb_typeof(entry.value)<>'number' THEN RETURN false;END IF;v:=entry.value::text::numeric;
  IF v<0 OR v>9007199254740991 OR v<>trunc(v) THEN RETURN false;END IF;
 END LOOP;
 IF (estimate->>'ram_bytes')::numeric<1073741824 OR (estimate->>'vram_bytes')::numeric<>0
 OR (estimate->>'pixel_count')::numeric NOT BETWEEN 1 AND 200000000 OR (estimate->>'output_bytes')::numeric<1048576 THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;
 IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=n.node_key AND old.operation=n.operation AND old.params_artifact_id=params_id
  AND old.input_digest=p_task->>'input_digest' AND old.inputs=n.inputs AND old.execution_profile=profile;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(n.id,p_tenant,p_run,n.node_key,'export.encode','1',p_task->>'resource_class','blocked',n.inputs,params_id,p_task->>'input_digest',profile);
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_download_export_task(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_download_export_task(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_download_export_task(uuid,uuid,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.seal_download_exports(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;j photo.download_jobs%ROWTYPE;expected bigint;
BEGIN
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO j FROM photo.download_jobs WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'download' OR r.cancel_requested OR j.cancel_requested OR r.status NOT IN ('queued','running') THEN RETURN false;END IF;
 IF r.graph_sealed THEN RETURN true;END IF;
 SELECT item_count INTO expected FROM photo.snapshots WHERE tenant_id=p_tenant AND id=r.input_snapshot_id AND kind='selection';
 IF expected NOT BETWEEN 1 AND 5000 OR (SELECT count(*) FROM photo.download_export_plans WHERE tenant_id=p_tenant AND run_id=p_run)<>expected
 OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='export.encode')<>expected
 OR EXISTS(SELECT FROM photo.download_export_plans p LEFT JOIN photo.tasks t ON t.tenant_id=p.tenant_id AND t.id=p.id
  WHERE p.tenant_id=p_tenant AND p.run_id=p_run AND (t.id IS NULL OR t.state<>'blocked' OR t.inputs<>p.inputs)) THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready',next_attempt_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_run AND operation='export.encode' AND state='blocked';
 UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1,download_build_token=NULL,download_build_until=NULL WHERE tenant_id=p_tenant AND id=p_run;
 UPDATE photo.download_jobs SET status='building' WHERE tenant_id=p_tenant AND id=j.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_download_exports(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_download_exports(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_download_exports(uuid,uuid) TO photo_scheduler;

-- Generalize nested export inputs to every registry operation whose v1 contract
-- returns WorkingImage; each nested ref still has to occur in the same canonical result.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.download_export_inputs(p_tenant uuid,p_run uuid)
RETURNS TABLE(ordinal bigint,role text,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT si.ordinal,a.output_role,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type
 FROM photo.runs r JOIN photo.download_jobs j ON j.tenant_id=r.tenant_id AND j.run_id=r.id AND j.snapshot_id=r.input_snapshot_id
 JOIN photo.snapshot_items si ON si.tenant_id=r.tenant_id AND si.snapshot_id=r.input_snapshot_id
 JOIN photo.artifacts master ON master.tenant_id=si.tenant_id AND master.id=si.artifact_id AND master.sha256=si.sha256
 JOIN photo.tasks t ON t.tenant_id=master.tenant_id AND t.id=master.created_by_task_id AND t.state='succeeded'
  AND t.operation IN ('asset.decode','image.denoise','image.color','image.local','portrait.beauty','portrait.detail','defects.repair','background.compose','background.blur','background.sky','image.upscale','hdr.merge','hdr.window_pull','derivative.resize','derivative.sharpen','derivative.watermark')
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.output_role IN ('working_rgb','alpha')
 WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.kind='download' AND r.status IN ('queued','running') AND NOT r.cancel_requested
 AND NOT j.cancel_requested AND j.status IN ('queued','preparing') AND master.state='ready' AND master.output_role='working_image'
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',master.id::text,'sha256',master.sha256))
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 AND a.state='ready' AND a.object_version IS NOT NULL AND a.object_version NOT IN ('','null') AND a.media_type='application/octet-stream'
 AND a.bytes>0 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY si.ordinal,a.output_role,a.id
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.download_export_inputs(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.download_export_inputs(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.download_export_inputs(uuid,uuid) TO photo_scheduler;

-- Extend the existing fenced publisher only for registered download plan owners.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.reserve_backend_document(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
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
 OR (p_purpose='task_params' AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_owner AND e.state='open' AND e.kind LIKE 'review.defects/%') AND NOT photo.download_export_document_owner(p_tenant,p_run,p_owner)) THEN
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
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.complete_backend_document(p_tenant uuid,p_run uuid,p_owner uuid,p_purpose text,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE; r photo.runs%ROWTYPE; target text;
BEGIN
 IF p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 OR p_token IS NULL THEN RETURN false;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.cancel_requested OR r.status NOT IN ('queued','running','waiting_review') THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents b WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.owner_id=p_owner AND b.purpose=p_purpose FOR UPDATE;
 IF NOT FOUND OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket THEN RETURN false;END IF;
 IF (p_purpose='review_selection' AND NOT EXISTS(SELECT FROM photo.reviews v WHERE v.tenant_id=p_tenant AND v.run_id=p_run AND v.id=p_owner AND v.kind='defects' AND v.state='decided'))
 OR (p_purpose='task_params' AND NOT EXISTS(SELECT FROM photo.graph_expansions e WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.id=p_owner AND e.state='open' AND e.kind LIKE 'review.defects/%') AND NOT photo.download_export_document_owner(p_tenant,p_run,p_owner)) THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts a WHERE a.id=d.artifact_id AND a.tenant_id=d.tenant_id AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=p_version);END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(d.artifact_id,d.tenant_id,d.bucket,target,p_version,d.sha256,d.bytes,'application/json','ready',clock_timestamp(),d.purpose);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-d.bytes,storage_bytes=storage_bytes+d.bytes WHERE tenant_id=d.tenant_id AND reserved_bytes>=d.bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;
 RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
