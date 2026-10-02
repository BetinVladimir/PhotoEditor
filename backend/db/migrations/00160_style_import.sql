-- Asynchronous, version-pinned import of untrusted portable StyleV2 packages.
-- +goose Up
INSERT INTO photo.operation_registry(operation,version,resource_class,routing_key,model_id,timeout_seconds,backends)
VALUES('style.import','1','cpu','cpu.style_import',NULL,300,'["cpu"]');
INSERT INTO photo.operation_output_policies(operation,version,role,media_types,max_bytes,max_count) VALUES
 ('style.import','1','style_candidate','["application/json"]',104857600,1),
 ('style.import','1','aggregate_report','["application/json"]',1048576,1),
 ('style.import','1','style_asset','["image/png","image/jpeg","application/octet-stream"]',104857600,128);

CREATE TABLE photo.style_import_builds(
 tenant_id uuid NOT NULL,run_id uuid NOT NULL PRIMARY KEY,style_id uuid NOT NULL,task_id uuid NOT NULL UNIQUE,
 package_artifact_id uuid NOT NULL,manifest_artifact_id uuid,params jsonb NOT NULL,params_sha256 text NOT NULL,
 state text NOT NULL DEFAULT 'queued',lease_token uuid,lease_until timestamptz,last_error text,
 UNIQUE(tenant_id,run_id),FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id),
 FOREIGN KEY(tenant_id,style_id) REFERENCES photo.styles(tenant_id,id),
 FOREIGN KEY(tenant_id,package_artifact_id) REFERENCES photo.artifacts(tenant_id,id),
 FOREIGN KEY(tenant_id,manifest_artifact_id) REFERENCES photo.artifacts(tenant_id,id),
 CHECK(state IN('queued','building','complete','failed')),CHECK(params_sha256 ~ '^[0-9a-f]{64}$'),CHECK(jsonb_typeof(params)='object')
);
ALTER TABLE photo.style_import_builds ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.style_import_builds FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON photo.style_import_builds TO photo_api USING(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=nullif(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY trusted_functions ON photo.style_import_builds TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE ON photo.style_import_builds TO photo_api;
GRANT SELECT,INSERT,UPDATE ON photo.style_import_builds TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.claim_style_import_build() RETURNS TABLE(tenant_id uuid,run_id uuid,style_id uuid,task_id uuid,package_artifact_id uuid,manifest_artifact_id uuid,params jsonb,params_sha256 text,token uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.style_import_builds%ROWTYPE;
BEGIN
 SELECT * INTO b FROM photo.style_import_builds x WHERE x.state IN('queued','building') AND (x.state='queued' OR x.lease_until<=clock_timestamp()) ORDER BY x.run_id LIMIT 1 FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN;END IF;
 UPDATE photo.style_import_builds SET state='building',lease_token=gen_random_uuid(),lease_until=clock_timestamp()+interval '60 seconds' WHERE run_id=b.run_id RETURNING * INTO b;
 RETURN QUERY SELECT b.tenant_id,b.run_id,b.style_id,b.task_id,b.package_artifact_id,b.manifest_artifact_id,b.params,b.params_sha256,b.lease_token;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.claim_style_import_build() OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.claim_style_import_build() FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.claim_style_import_build() TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_style_import_document(p_tenant uuid,p_run uuid,p_owner uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;b photo.style_import_builds%ROWTYPE;capacity numeric;used numeric;aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();
BEGIN
 IF p_sha !~ '^[0-9a-f]{64}$' OR p_bytes NOT BETWEEN 2 AND 1048576 OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023';END IF;
 SELECT * INTO b FROM photo.style_import_builds WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=p_owner AND state='building' AND lease_until>=clock_timestamp();
 IF NOT FOUND OR b.params_sha256<>p_sha THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN
  IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;
 END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket) VALUES(p_tenant,p_run,p_owner,'task_params',aid,fence,p_sha,p_bytes,p_bucket);
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_style_import_document(uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.reserve_style_import_document(uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.reserve_style_import_document(uuid,uuid,uuid,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_style_import_document(p_tenant uuid,p_run uuid,p_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;b photo.style_import_builds%ROWTYPE;target text;
BEGIN
 IF p_version IS NULL OR p_version IN('','null') THEN RETURN false;END IF;PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO b FROM photo.style_import_builds WHERE tenant_id=p_tenant AND run_id=p_run AND task_id=p_owner AND state='building' AND lease_until>=clock_timestamp();
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF b.run_id IS NULL OR d.id IS NULL OR d.artifact_id<>p_artifact OR d.token<>p_token OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket OR b.params_sha256<>p_sha THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version;END IF;target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_style_import_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.complete_style_import_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.complete_style_import_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.materialize_style_import_task(p_tenant uuid,p_run uuid,p_token uuid,p_params uuid,p_params_sha text,p_digest text,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.style_import_builds%ROWTYPE;inputs jsonb;old photo.tasks%ROWTYPE;
BEGIN
 SELECT * INTO b FROM photo.style_import_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.state<>'building' OR b.lease_token<>p_token OR b.lease_until<clock_timestamp() OR b.params_sha256<>p_params_sha OR p_digest !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=b.task_id AND d.state='ready' AND a.id=p_params AND a.sha256=p_params_sha AND a.state='ready') THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',id::text,'sha256',sha256) ORDER BY id) INTO inputs FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(b.package_artifact_id,b.manifest_artifact_id) AND state='ready' AND object_version IS NOT NULL AND (expires_at IS NULL OR expires_at>clock_timestamp());
 IF jsonb_array_length(inputs)<>(CASE WHEN b.manifest_artifact_id IS NULL THEN 1 ELSE 2 END) OR jsonb_typeof(p_profile)<>'object' THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=b.task_id;IF FOUND THEN RETURN old.run_id=p_run AND old.input_digest=p_digest;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,timeout_seconds,next_attempt_at) VALUES(b.task_id,p_tenant,p_run,'style.import','style.import','1','cpu','ready',inputs,p_params,p_digest,p_profile,300,clock_timestamp());
 UPDATE photo.runs SET graph_sealed=true,status='running',revision=revision+1 WHERE tenant_id=p_tenant AND id=p_run AND NOT graph_sealed;
 UPDATE photo.style_import_builds SET state='complete',lease_until=clock_timestamp() WHERE run_id=p_run;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_style_import_task(uuid,uuid,uuid,uuid,text,text,jsonb) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.materialize_style_import_task(uuid,uuid,uuid,uuid,text,text,jsonb) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.materialize_style_import_task(uuid,uuid,uuid,uuid,text,text,jsonb) TO photo_scheduler;

-- Let the existing candidate materializer consume successful import results.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.claim_style_candidate()
RETURNS TABLE(candidate_id uuid,token uuid,tenant_id uuid,run_id uuid,style_id uuid,config_id uuid,config_bucket text,config_sha text,config_key text,config_version text,config_bytes bigint,config_media text,report_id uuid,report_bucket text,report_sha text,report_key text,report_version text,report_bytes bigint,report_media text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE picked record;c photo.style_candidates%ROWTYPE;
BEGIN
 SELECT r.tenant_id,r.id run_id,(r.config_snapshot->>'style_id')::uuid style_id,t.id task_id,z.outputs INTO picked FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id AND t.operation IN('style.aggregate','style.import') AND t.operation_version='1' AND t.state='succeeded' JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.id=t.canonical_result_id AND z.task_id=t.id LEFT JOIN photo.style_candidates existing ON existing.tenant_id=r.tenant_id AND existing.training_run_id=r.id WHERE r.kind IN('analyze','style_import') AND r.config_snapshot ? 'style_id' AND existing.id IS NULL AND NOT r.cancel_requested ORDER BY r.created_at,r.id LIMIT 1 FOR UPDATE OF r SKIP LOCKED;
 IF FOUND THEN INSERT INTO photo.style_candidates(tenant_id,style_id,training_run_id,config_artifact_id,report_artifact_id) SELECT picked.tenant_id,picked.style_id,picked.run_id,config.id,report.id FROM photo.artifacts config,photo.artifacts report WHERE config.tenant_id=picked.tenant_id AND config.created_by_task_id=picked.task_id AND config.output_role='style_candidate' AND config.state='ready' AND report.tenant_id=picked.tenant_id AND report.created_by_task_id=picked.task_id AND report.output_role='aggregate_report' AND report.state='ready' AND picked.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',config.id::text,'sha256',config.sha256)) AND picked.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',report.id::text,'sha256',report.sha256)) RETURNING * INTO c;END IF;
 IF c.id IS NULL THEN SELECT * INTO c FROM photo.style_candidates x WHERE x.state='materializing' AND x.materialize_lease_until<=clock_timestamp() ORDER BY x.created_at,x.id LIMIT 1 FOR UPDATE SKIP LOCKED;IF c.id IS NOT NULL THEN UPDATE photo.style_candidates SET materialize_token=gen_random_uuid(),materialize_lease_until=clock_timestamp()+interval '60 seconds' WHERE id=c.id RETURNING * INTO c;END IF;END IF;
 IF c.id IS NULL THEN RETURN;END IF;RETURN QUERY SELECT c.id,c.materialize_token,c.tenant_id,c.training_run_id,c.style_id,ca.id,ca.bucket,ca.sha256,ca.object_key,ca.object_version,ca.bytes,ca.media_type,ra.id,ra.bucket,ra.sha256,ra.object_key,ra.object_version,ra.bytes,ra.media_type FROM photo.artifacts ca,photo.artifacts ra WHERE ca.tenant_id=c.tenant_id AND ca.id=c.config_artifact_id AND ca.state='ready' AND ca.object_version IS NOT NULL AND ra.tenant_id=c.tenant_id AND ra.id=c.report_artifact_id AND ra.state='ready' AND ra.object_version IS NOT NULL;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
