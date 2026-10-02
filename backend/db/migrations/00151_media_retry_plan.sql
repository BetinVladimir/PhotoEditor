-- +goose Up
CREATE TABLE photo.media_retry_nodes (
 id uuid PRIMARY KEY,tenant_id uuid NOT NULL,run_id uuid NOT NULL,event_id uuid NOT NULL,
 asset_version_id uuid NOT NULL,variant text NOT NULL,operation text NOT NULL,profile_hash text NOT NULL,inputs jsonb NOT NULL,params_sha256 text NOT NULL,
 UNIQUE(tenant_id,run_id,variant),UNIQUE(tenant_id,event_id,variant),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,asset_version_id) REFERENCES photo.asset_versions(tenant_id,id),
 CHECK(variant IN('thumb320','thumb640','preview1600','preview2560')),
 CHECK(operation=CASE WHEN variant LIKE 'thumb%' THEN 'media.thumbnail' ELSE 'media.preview' END),
 CHECK(jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) BETWEEN 1 AND 3),
 CHECK(profile_hash ~ '^[0-9a-f]{64}$'),CHECK(params_sha256 ~ '^[0-9a-f]{64}$')
);
ALTER TABLE photo.media_retry_nodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE photo.media_retry_nodes FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.media_retry_nodes TO photo_fn_owner USING(true) WITH CHECK(true);
GRANT SELECT,INSERT,UPDATE,DELETE ON photo.media_retry_nodes TO photo_fn_owner;

-- +goose StatementBegin
CREATE FUNCTION photo.begin_media_retry_plan(p_event uuid,p_token uuid,p_run uuid,p_hash text,p_config jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;b photo.bulk_operations%ROWTYPE;r photo.runs%ROWTYPE;
BEGIN
 IF p_hash !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_config)<>'object' OR p_config<>jsonb_build_object('event_id',p_event,'asset_version_id',p_config->'asset_version_id','variants',p_config->'variants','srgb_sha256',p_config->'srgb_sha256') THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=p_event FOR UPDATE;
 IF NOT FOUND OR j.status<>'materializing' OR j.materialize_token<>p_token OR j.materialize_lease_until<=clock_timestamp() OR j.asset_version_id<>(p_config->>'asset_version_id')::uuid OR j.variants<>p_config->'variants' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.bulk_operations WHERE tenant_id=j.tenant_id AND id=j.bulk_operation_id;
 IF NOT FOUND OR b.cancel_requested THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=j.tenant_id AND id=p_run;
 IF FOUND THEN RETURN r.kind='preview' AND r.input_snapshot_id=b.snapshot_id AND r.config_hash=p_hash AND r.config_snapshot=p_config AND r.created_by=b.created_by AND NOT r.graph_sealed;END IF;
 IF EXISTS(SELECT FROM photo.runs WHERE id=p_run) THEN RETURN false;END IF;
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(p_run,j.tenant_id,'preview','queued',b.snapshot_id,p_config,p_hash,b.created_by);
 UPDATE photo.media_retry_jobs SET run_id=p_run WHERE event_id=p_event;RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.register_media_retry_node(p_event uuid,p_token uuid,p_run uuid,p_node jsonb,p_bound_event uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;old photo.media_retry_nodes%ROWTYPE;x jsonb;expected text;
BEGIN
 IF p_event<>p_bound_event OR jsonb_typeof(p_node)<>'object' OR (p_node-'id'-'asset_version_id'-'variant'-'operation'-'profile_hash'-'inputs'-'params_sha256')<>'{}' OR jsonb_typeof(p_node->'inputs')<>'array' THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=p_event FOR UPDATE;
 IF NOT FOUND OR j.status<>'materializing' OR j.materialize_token<>p_token OR j.materialize_lease_until<=clock_timestamp() OR j.run_id<>p_run OR j.asset_version_id<>(p_node->>'asset_version_id')::uuid THEN RETURN false;END IF;
 expected:=CASE WHEN p_node->>'variant' LIKE 'thumb%' THEN 'media.thumbnail' ELSE 'media.preview' END;
 IF p_node->>'variant' NOT IN('thumb320','thumb640','preview1600','preview2560') OR p_node->>'operation'<>expected OR p_node->>'profile_hash' !~ '^[0-9a-f]{64}$' OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF (p_node->>'variant' LIKE 'thumb%' AND NOT (j.variants ? 'thumbnail')) OR (p_node->>'variant' LIKE 'preview%' AND NOT (j.variants ? 'preview')) THEN RETURN false;END IF;
 FOR x IN SELECT value FROM jsonb_array_elements(p_node->'inputs') LOOP IF (x-'artifact_id'-'sha256')<>'{}' OR x->>'sha256' !~ '^[0-9a-f]{64}$' OR NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=j.tenant_id AND a.id=(x->>'artifact_id')::uuid AND a.sha256=x->>'sha256' AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN false;END IF;END LOOP;
 IF (p_node->'inputs'->0->>'artifact_id')::uuid<>(SELECT master_artifact_id FROM photo.asset_versions WHERE tenant_id=j.tenant_id AND id=j.asset_version_id) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.media_retry_nodes WHERE tenant_id=j.tenant_id AND id=(p_node->>'id')::uuid;
 IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'-'event_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.media_retry_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.media_retry_nodes(id,tenant_id,run_id,event_id,asset_version_id,variant,operation,profile_hash,inputs,params_sha256) VALUES((p_node->>'id')::uuid,j.tenant_id,p_run,p_event,j.asset_version_id,p_node->>'variant',p_node->>'operation',p_node->>'profile_hash',p_node->'inputs',p_node->>'params_sha256');RETURN true;
EXCEPTION WHEN invalid_text_representation THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_media_retry_node(uuid,uuid,uuid,jsonb,uuid) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.register_media_retry_node(uuid,uuid,uuid,jsonb,uuid) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.register_media_retry_node(uuid,uuid,uuid,jsonb,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.reserve_media_retry_document(p_tenant uuid,p_run uuid,p_owner uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;n photo.media_retry_nodes%ROWTYPE;j photo.media_retry_jobs%ROWTYPE;capacity numeric;used numeric;aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();
BEGIN
 IF p_sha !~ '^[0-9a-f]{64}$' OR p_bytes NOT BETWEEN 2 AND 1048576 OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023';END IF;
 SELECT * INTO n FROM photo.media_retry_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_owner;SELECT * INTO j FROM photo.media_retry_jobs WHERE tenant_id=p_tenant AND event_id=n.event_id;
 IF n.id IS NULL OR j.status<>'materializing' OR j.materialize_lease_until<=clock_timestamp() OR n.params_sha256<>p_sha THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;IF d.state='ready' THEN IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=p_sha AND a.object_version=d.object_version) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;END IF;UPDATE photo.backend_documents SET token=fence WHERE id=d.id;RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket) VALUES(p_tenant,p_run,p_owner,'task_params',aid,fence,p_sha,p_bytes,p_bucket);UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_media_retry_document(uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.reserve_media_retry_document(uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.reserve_media_retry_document(uuid,uuid,uuid,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_media_retry_document(p_tenant uuid,p_run uuid,p_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;n photo.media_retry_nodes%ROWTYPE;j photo.media_retry_jobs%ROWTYPE;target text;
BEGIN
 IF p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO n FROM photo.media_retry_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_owner;SELECT * INTO j FROM photo.media_retry_jobs WHERE tenant_id=p_tenant AND event_id=n.event_id;SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF n.id IS NULL OR j.status<>'materializing' OR j.materialize_lease_until<=clock_timestamp() OR d.id IS NULL OR d.artifact_id<>p_artifact OR d.token<>p_token OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket OR n.params_sha256<>p_sha THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.sha256=p_sha AND a.object_version=p_version);END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_media_retry_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.complete_media_retry_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.complete_media_retry_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.materialize_media_retry_task(p_event uuid,p_token uuid,p_run uuid,p_task jsonb) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;n photo.media_retry_nodes%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;old photo.tasks%ROWTYPE;
BEGIN
 IF jsonb_typeof(p_task)<>'object' OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=p_event FOR UPDATE;SELECT * INTO n FROM photo.media_retry_nodes WHERE tenant_id=j.tenant_id AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF j.status<>'materializing' OR j.materialize_token<>p_token OR j.materialize_lease_until<=clock_timestamp() OR j.run_id<>p_run OR n.id IS NULL OR p_task->>'node_key'<>'media.retry/'||p_event||'/'||n.variant OR p_task->>'operation'<>n.operation OR p_task->'inputs'<>n.inputs OR p_task->>'operation_version'<>'1' OR p_task->>'resource_class'<>'media' OR p_task->>'input_digest' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=n.operation AND o.version='1' AND o.resource_class='media' AND o.timeout_seconds=(p_task->>'timeout_seconds')::int) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;IF NOT EXISTS(SELECT FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=j.tenant_id AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready' AND a.id=params_id AND a.state='ready' AND a.sha256=n.params_sha256 AND a.sha256=p_task->'params'->>'sha256' AND a.object_version IS NOT NULL) THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';IF jsonb_typeof(profile)<>'object' OR jsonb_typeof(estimate)<>'object' OR NOT(estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count']) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=j.tenant_id AND id=n.id;IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=p_task->>'node_key' AND old.operation=n.operation AND old.params_artifact_id=params_id AND old.inputs=n.inputs AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(n.id,j.tenant_id,p_run,p_task->>'node_key',n.operation,'1','media','blocked',n.inputs,params_id,p_task->>'input_digest',profile);RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_media_retry_task(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.materialize_media_retry_task(uuid,uuid,uuid,jsonb) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.materialize_media_retry_task(uuid,uuid,uuid,jsonb) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.seal_media_retry_plan(p_event uuid,p_token uuid,p_run uuid) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE j photo.media_retry_jobs%ROWTYPE;expected int;
BEGIN
 SELECT * INTO j FROM photo.media_retry_jobs WHERE event_id=p_event FOR UPDATE;IF NOT FOUND OR j.status<>'materializing' OR j.materialize_token<>p_token OR j.materialize_lease_until<=clock_timestamp() OR j.run_id<>p_run THEN RETURN false;END IF;
 expected:=jsonb_array_length(j.variants)*2;
 IF (SELECT count(*) FROM photo.media_retry_nodes WHERE tenant_id=j.tenant_id AND run_id=p_run)<>expected OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=j.tenant_id AND run_id=p_run)<>expected OR (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=j.tenant_id AND run_id=p_run AND purpose='task_params' AND state='ready')<>expected THEN RETURN false;END IF;
 UPDATE photo.tasks SET state='ready' WHERE tenant_id=j.tenant_id AND run_id=p_run AND state='blocked';UPDATE photo.runs SET status='running',graph_sealed=true,revision=revision+1 WHERE tenant_id=j.tenant_id AND id=p_run AND status='queued' AND NOT graph_sealed;IF NOT FOUND THEN RETURN false;END IF;
 UPDATE photo.media_retry_jobs SET status='running',materialize_token=NULL,materialize_lease_until=NULL WHERE event_id=p_event;RETURN true;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.seal_media_retry_plan(uuid,uuid,uuid) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
