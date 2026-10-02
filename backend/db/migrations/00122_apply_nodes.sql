-- Immutable, staged apply plans. A stage can only bind canonical outputs from
-- the immediately preceding stage, so graph sealing is preserved throughout.
-- +goose Up
CREATE TABLE photo.apply_nodes (
 id uuid PRIMARY KEY,tenant_id uuid NOT NULL,run_id uuid NOT NULL,phase text NOT NULL,
 ordinal int NOT NULL,source_item_id uuid NOT NULL,source_asset_id uuid NOT NULL,source_version_id uuid NOT NULL,
 node_key text NOT NULL,operation text NOT NULL,inputs jsonb NOT NULL,prerequisites uuid[] NOT NULL DEFAULT '{}',
 params_sha256 text NOT NULL,params jsonb NOT NULL,
 UNIQUE(tenant_id,run_id,node_key),UNIQUE(tenant_id,run_id,phase,ordinal),
 FOREIGN KEY(tenant_id,run_id) REFERENCES photo.runs(tenant_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(phase IN ('mask','stats','resolve','color','local')),
 CHECK(operation=CASE phase WHEN 'mask' THEN 'mask.person' WHEN 'stats' THEN 'color.stats' WHEN 'resolve' THEN 'style.resolve' WHEN 'color' THEN 'image.color' ELSE 'image.local' END),
 CHECK(ordinal BETWEEN 0 AND 4999),CHECK(params_sha256 ~ '^[0-9a-f]{64}$'),
 CHECK(jsonb_typeof(inputs)='array' AND jsonb_array_length(inputs) BETWEEN 1 AND 128),CHECK(jsonb_typeof(params)='object')
);
ALTER TABLE photo.apply_nodes ENABLE ROW LEVEL SECURITY;ALTER TABLE photo.apply_nodes FORCE ROW LEVEL SECURITY;
CREATE POLICY trusted_functions ON photo.apply_nodes TO photo_fn_owner USING(true) WITH CHECK(true);

-- +goose StatementBegin
CREATE FUNCTION photo.apply_inputs(p_tenant uuid,p_run uuid,p_token uuid)
RETURNS TABLE(ordinal bigint,item_id uuid,asset_id uuid,asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text,payload jsonb,config_snapshot jsonb,style_artifact_id uuid,style_sha256 text,style_bytes bigint)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT i.ordinal,i.item_id,v.asset_id,i.asset_version_id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,i.payload,r.config_snapshot,sa.id,sa.sha256,sa.bytes
 FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id
 JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256
 JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256 AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 JOIN photo.style_versions sv ON sv.tenant_id=b.tenant_id AND sv.id=b.style_version_id AND sv.approval_status='accepted'
 JOIN photo.artifacts sa ON sa.tenant_id=sv.tenant_id AND sa.id=sv.export_artifact_id AND sa.sha256=sv.config_hash AND sa.state='ready' AND sa.object_version IS NOT NULL
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp() AND b.phase IN ('mask','stats','resolve','color','local') ORDER BY i.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_inputs(uuid,uuid,uuid) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.apply_inputs(uuid,uuid,uuid) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.apply_inputs(uuid,uuid,uuid) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.apply_stage_outputs(p_tenant uuid,p_run uuid,p_token uuid,p_operation text,p_role text)
RETURNS TABLE(ordinal int,item_id uuid,task_id uuid,artifact_id uuid,sha256 text,bytes bigint,bucket text,object_key text,object_version text,media_type text)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 SELECT n.ordinal,n.source_item_id,t.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type FROM photo.apply_builds b
 JOIN photo.apply_nodes n ON n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.operation=p_operation
 JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.id=n.id AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 JOIN photo.task_results tr ON tr.tenant_id=t.tenant_id AND tr.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=t.tenant_id AND a.created_by_task_id=t.id AND a.produced_attempt=tr.attempt AND a.produced_fence=tr.fencing_token
  AND a.output_role=p_role AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE b.tenant_id=p_tenant AND b.run_id=p_run AND b.lease_token=p_token AND b.lease_until>=clock_timestamp()
  AND ((b.phase IN ('stats','resolve','color','local') AND p_operation='mask.person' AND p_role='person_mask_ref')
   OR (b.phase='resolve' AND p_operation='color.stats' AND p_role='stats')
   OR (b.phase IN ('color','local') AND p_operation='style.resolve' AND p_role='resolved_style')
   OR (b.phase='local' AND p_operation='image.color' AND p_role='working_image'))
  AND tr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) ORDER BY n.ordinal
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.apply_stage_outputs(uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.apply_stage_outputs(uuid,uuid,uuid,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.apply_stage_outputs(uuid,uuid,uuid,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.register_apply_node(p_tenant uuid,p_run uuid,p_token uuid,p_node jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;old photo.apply_nodes%ROWTYPE;entry jsonb;expected text;
BEGIN
 IF p_node IS NULL OR jsonb_typeof(p_node)<>'object' OR octet_length(p_node::text)>1048576
 OR (p_node-'id'-'phase'-'ordinal'-'source_item_id'-'source_asset_id'-'source_version_id'-'node_key'-'operation'-'inputs'-'prerequisites'-'params_sha256'-'params')<>'{}'
 OR p_node->>'params_sha256' !~ '^[0-9a-f]{64}$' OR jsonb_typeof(p_node->'inputs')<>'array' OR jsonb_typeof(p_node->'prerequisites')<>'array' OR jsonb_typeof(p_node->'params')<>'object' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;
 IF NOT FOUND OR b.lease_token IS DISTINCT FROM p_token OR b.lease_until<clock_timestamp() OR b.phase<>p_node->>'phase' OR b.phase NOT IN ('mask','stats','resolve','color','local') THEN RETURN false;END IF;
 expected:=CASE b.phase WHEN 'mask' THEN 'mask.person' WHEN 'stats' THEN 'color.stats' WHEN 'resolve' THEN 'style.resolve' WHEN 'color' THEN 'image.color' ELSE 'image.local' END;
 IF p_node->>'operation'<>expected OR (p_node->>'ordinal')::int NOT BETWEEN 0 AND b.item_count-1 THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(p_node->'inputs') LOOP IF (entry-'artifact_id'-'sha256')<>'{}' OR entry->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;END LOOP;
 IF NOT EXISTS(SELECT FROM photo.runs r JOIN photo.snapshot_items i ON i.tenant_id=r.tenant_id AND i.snapshot_id=r.input_snapshot_id JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id
  WHERE r.tenant_id=p_tenant AND r.id=p_run AND i.ordinal=(p_node->>'ordinal')::int AND i.item_id=(p_node->>'source_item_id')::uuid AND v.asset_id=(p_node->>'source_asset_id')::uuid AND v.id=(p_node->>'source_version_id')::uuid) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements_text(p_node->'prerequisites') x LEFT JOIN photo.tasks t ON t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=x::uuid AND t.state='succeeded' WHERE t.id IS NULL) THEN RETURN false;END IF;
 -- Every dynamically bound predecessor artifact must be both produced by the
 -- exact same-ordinal stage and present in that task's canonical result.
 IF b.phase='stats' AND NOT EXISTS(SELECT FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id JOIN photo.artifacts pa ON pa.tenant_id=pt.tenant_id AND pa.id=(p_node->'params'->'person'->'artifact'->>'artifact_id')::uuid AND pa.sha256=p_node->'params'->'person'->'artifact'->>'sha256' AND pa.created_by_task_id=pt.id AND pa.produced_attempt=pr.attempt AND pa.produced_fence=pr.fencing_token AND pa.output_role='person_mask' AND pa.state='ready' WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase='mask' AND pn.ordinal=(p_node->>'ordinal')::int AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256))) THEN RETURN false;END IF;
 IF b.phase='resolve' AND (SELECT (r.config_snapshot->>'person_mask_required')::boolean FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run) AND NOT EXISTS(SELECT FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id JOIN photo.artifacts pa ON pa.tenant_id=pt.tenant_id AND pa.id=(p_node->'params'->'stats'->>'artifact_id')::uuid AND pa.sha256=p_node->'params'->'stats'->>'sha256' AND pa.created_by_task_id=pt.id AND pa.produced_attempt=pr.attempt AND pa.produced_fence=pr.fencing_token AND pa.output_role='stats' AND pa.state='ready' WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase='stats' AND pn.ordinal=(p_node->>'ordinal')::int AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256))) THEN RETURN false;END IF;
 IF b.phase='color' AND NOT EXISTS(SELECT FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id JOIN photo.artifacts pa ON pa.tenant_id=pt.tenant_id AND pa.id=(p_node->'params'->'resolved'->>'artifact_id')::uuid AND pa.sha256=p_node->'params'->'resolved'->>'sha256' AND pa.created_by_task_id=pt.id AND pa.produced_attempt=pr.attempt AND pa.produced_fence=pr.fencing_token AND pa.output_role='resolved_style' AND pa.state='ready' WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase='resolve' AND pn.ordinal=(p_node->>'ordinal')::int AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256))) THEN RETURN false;END IF;
 IF b.phase='local' AND (NOT EXISTS(SELECT FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id JOIN photo.artifacts pa ON pa.tenant_id=pt.tenant_id AND pa.id=(p_node->'params'->'image'->>'artifact_id')::uuid AND pa.sha256=p_node->'params'->'image'->>'sha256' AND pa.created_by_task_id=pt.id AND pa.produced_attempt=pr.attempt AND pa.produced_fence=pr.fencing_token AND pa.output_role='working_image' AND pa.state='ready' WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase='color' AND pn.ordinal=(p_node->>'ordinal')::int AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256))) OR NOT EXISTS(SELECT FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id JOIN photo.artifacts pa ON pa.tenant_id=pt.tenant_id AND pa.id=(p_node->'params'->'resolved'->>'artifact_id')::uuid AND pa.sha256=p_node->'params'->'resolved'->>'sha256' AND pa.created_by_task_id=pt.id AND pa.produced_attempt=pr.attempt AND pa.produced_fence=pr.fencing_token AND pa.output_role='resolved_style' AND pa.state='ready' WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase='resolve' AND pn.ordinal=(p_node->>'ordinal')::int AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256)))) THEN RETURN false;END IF;
 IF b.phase='local' AND (SELECT (r.config_snapshot->>'person_mask_required')::boolean FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run) AND NOT EXISTS(SELECT FROM photo.apply_nodes pn JOIN photo.tasks pt ON pt.tenant_id=pn.tenant_id AND pt.id=pn.id AND pt.state='succeeded' JOIN photo.task_results pr ON pr.tenant_id=pt.tenant_id AND pr.id=pt.canonical_result_id JOIN photo.artifacts pa ON pa.tenant_id=pt.tenant_id AND pa.id=(p_node->'params'->'person'->'artifact'->>'artifact_id')::uuid AND pa.sha256=p_node->'params'->'person'->'artifact'->>'sha256' AND pa.created_by_task_id=pt.id AND pa.produced_attempt=pr.attempt AND pa.produced_fence=pr.fencing_token AND pa.output_role='person_mask' AND pa.state='ready' WHERE pn.tenant_id=p_tenant AND pn.run_id=p_run AND pn.phase='mask' AND pn.ordinal=(p_node->>'ordinal')::int AND pr.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',pa.id,'sha256',pa.sha256))) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'id')::uuid;
 IF FOUND THEN RETURN to_jsonb(old)-'tenant_id'-'run_id'=p_node;END IF;
 IF EXISTS(SELECT FROM photo.apply_nodes WHERE id=(p_node->>'id')::uuid) THEN RETURN false;END IF;
 INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params)
 VALUES((p_node->>'id')::uuid,p_tenant,p_run,p_node->>'phase',(p_node->>'ordinal')::int,(p_node->>'source_item_id')::uuid,(p_node->>'source_asset_id')::uuid,(p_node->>'source_version_id')::uuid,p_node->>'node_key',p_node->>'operation',p_node->'inputs',ARRAY(SELECT jsonb_array_elements_text(p_node->'prerequisites')::uuid),p_node->>'params_sha256',p_node->'params');RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.register_apply_node(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.register_apply_node(uuid,uuid,uuid,jsonb) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.register_apply_node(uuid,uuid,uuid,jsonb) TO photo_scheduler;

-- Apply parameter documents use the same immutable protocol but a narrower authorization predicate.
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_apply_document(p_tenant uuid,p_run uuid,p_owner uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS TABLE(outcome text,artifact_id uuid,token uuid,ready boolean) LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;n photo.apply_nodes%ROWTYPE;capacity numeric;used numeric;aid uuid:=gen_random_uuid();fence uuid:=gen_random_uuid();BEGIN
 IF p_sha !~ '^[0-9a-f]{64}$' OR p_bytes NOT BETWEEN 2 AND 1048576 OR length(p_bucket) NOT BETWEEN 3 AND 63 THEN RAISE EXCEPTION 'INVALID_DOCUMENT' USING ERRCODE='22023';END IF;
 SELECT * INTO n FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_owner;IF NOT FOUND OR n.params_sha256<>p_sha THEN RETURN QUERY SELECT 'forbidden',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN IF d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket THEN RETURN QUERY SELECT 'conflict',NULL::uuid,NULL::uuid,false;RETURN;END IF;
  IF d.state='ready' THEN IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.state='ready' AND a.sha256=d.sha256 AND a.object_version=d.object_version) THEN RETURN QUERY SELECT 'stale',NULL::uuid,NULL::uuid,false;RETURN;END IF;RETURN QUERY SELECT 'replayed',d.artifact_id,d.token,true;RETURN;END IF;
  UPDATE photo.backend_documents SET token=fence WHERE id=d.id;RETURN QUERY SELECT 'resumed',d.artifact_id,fence,false;RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used+p_bytes>capacity THEN RETURN QUERY SELECT 'quota',NULL::uuid,NULL::uuid,false;RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket) VALUES(p_tenant,p_run,p_owner,'task_params',aid,fence,p_sha,p_bytes,p_bucket);UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+p_bytes WHERE tenant_id=p_tenant;RETURN QUERY SELECT 'created',aid,fence,false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.reserve_apply_document(uuid,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.reserve_apply_document(uuid,uuid,uuid,text,bigint,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.reserve_apply_document(uuid,uuid,uuid,text,bigint,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.complete_apply_document(p_tenant uuid,p_run uuid,p_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d photo.backend_documents%ROWTYPE;n photo.apply_nodes%ROWTYPE;target text;BEGIN
 IF p_version IS NULL OR p_version IN ('','null') OR length(p_version)>1024 THEN RETURN false;END IF;PERFORM id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO n FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_owner;SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF n.id IS NULL OR d.id IS NULL OR d.artifact_id<>p_artifact OR d.token<>p_token OR d.sha256<>p_sha OR d.bytes<>p_bytes OR d.bucket<>p_bucket OR n.params_sha256<>p_sha THEN RETURN false;END IF;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact AND a.state='ready' AND a.sha256=p_sha AND a.object_version=p_version);END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||p_artifact;INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role) VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;IF NOT FOUND THEN RAISE EXCEPTION 'DOCUMENT_QUOTA_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE id=d.id;RETURN true;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.complete_apply_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.complete_apply_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.complete_apply_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_scheduler;

-- +goose StatementBegin
CREATE FUNCTION photo.materialize_apply_task(p_tenant uuid,p_run uuid,p_token uuid,p_task jsonb) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.apply_builds%ROWTYPE;n photo.apply_nodes%ROWTYPE;params_id uuid;profile jsonb;estimate jsonb;entry record;old photo.tasks%ROWTYPE;BEGIN
 IF p_task IS NULL OR jsonb_typeof(p_task)<>'object' OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}' THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR UPDATE;SELECT * INTO n FROM photo.apply_nodes WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_task->>'id')::uuid;
 IF b.run_id IS NULL OR n.id IS NULL OR b.lease_token<>p_token OR b.lease_until<clock_timestamp() OR b.phase<>n.phase OR p_task->>'node_key'<>n.node_key OR p_task->>'operation'<>n.operation OR p_task->'inputs'<>n.inputs OR p_task->>'operation_version'<>'1' THEN RETURN false;END IF;
 IF NOT EXISTS(SELECT FROM photo.operation_registry o WHERE o.operation=n.operation AND o.version='1' AND o.resource_class=p_task->>'resource_class' AND o.timeout_seconds=(p_task->>'timeout_seconds')::int) THEN RETURN false;END IF;
 params_id:=(p_task->'params'->>'artifact_id')::uuid;IF NOT EXISTS(SELECT FROM photo.backend_documents d JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=p_tenant AND d.run_id=p_run AND d.owner_id=n.id AND d.purpose='task_params' AND d.state='ready' AND a.id=params_id AND a.state='ready' AND a.sha256=n.params_sha256 AND a.sha256=p_task->'params'->>'sha256' AND a.object_version IS NOT NULL) THEN RETURN false;END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements(n.inputs) x LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(x->>'artifact_id')::uuid AND a.sha256=x->>'sha256' AND a.state='ready' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE a.id IS NULL) THEN RETURN false;END IF;
 profile:=p_task->'execution_profile';estimate:=profile->'resource_estimate';IF jsonb_typeof(profile)<>'object' OR jsonb_typeof(estimate)<>'object' OR NOT (estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count']) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=n.id;IF FOUND THEN RETURN old.run_id=p_run AND old.node_key=n.node_key AND old.operation=n.operation AND old.params_artifact_id=params_id AND old.inputs=n.inputs AND old.input_digest=p_task->>'input_digest' AND old.execution_profile=profile;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) VALUES(n.id,p_tenant,p_run,n.node_key,n.operation,'1',p_task->>'resource_class','blocked',n.inputs,params_id,p_task->>'input_digest',profile);
 FOR entry IN SELECT unnest(n.prerequisites) AS value LOOP INSERT INTO photo.task_edges(tenant_id,run_id,prerequisite_id,dependent_id,condition) VALUES(p_tenant,p_run,entry.value,n.id,'success') ON CONFLICT(tenant_id,prerequisite_id,dependent_id) DO NOTHING;END LOOP;RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.materialize_apply_task(uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;REVOKE ALL ON FUNCTION photo.materialize_apply_task(uuid,uuid,uuid,jsonb) FROM PUBLIC;GRANT EXECUTE ON FUNCTION photo.materialize_apply_task(uuid,uuid,uuid,jsonb) TO photo_scheduler;

-- +goose Down
-- Forward-only production migrations. Restore a backup for destructive rollback.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
