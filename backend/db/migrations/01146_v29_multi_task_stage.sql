-- +goose Up
-- Durable blocked multi shell. Admission and transport remain separate gates.
ALTER TABLE photo.runs ADD COLUMN v29_multi_descriptor_jcs text
 CHECK(v29_multi_descriptor_jcs IS NULL OR octet_length(v29_multi_descriptor_jcs) BETWEEN 2 AND 1048576);
-- +goose StatementBegin
CREATE FUNCTION photo.stage_v29_multi_task(p_tenant uuid,p_run uuid,p_descriptor text,p_proof jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;request jsonb;plan jsonb;task jsonb;entries jsonb;expected jsonb;
 item jsonb;ref jsonb;aid uuid;prior uuid;contract_name text;body text:='[';first_item boolean:=true;
 old photo.tasks%ROWTYPE;params photo.backend_documents%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 needed numeric;capacity numeric;used numeric;matched integer;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;task:=plan->'task';entries:=plan->'entries';
 IF jsonb_typeof(plan) IS DISTINCT FROM 'object' OR plan->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(plan) k) IS DISTINCT FROM ARRAY['entries','reserved_bytes','schema_version','task']::text[]
 OR jsonb_typeof(task) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(task) k) IS DISTINCT FROM ARRAY['execution_profile','id','input_digest','node_key','operation','operation_version','params_artifact_id','params_sha','resource_class']::text[]
 OR coalesce(task->>'operation','') NOT IN('optical.panorama','optical.focus_stack') OR task->>'operation_version' IS DISTINCT FROM '1'
 OR task->>'resource_class' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 -- Scope validation takes tenant/usage/source locks before the run lock.
 IF photo.v29_multi_params_document_scope(p_tenant,p_run,(task->>'id')::uuid,'task_params',task->>'params_sha',octet_length(p_proof->>'params_jcs'),p_proof) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 request:=r.config_snapshot->'v29_multi_plan';
 IF request IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested
 OR task->>'id' IS DISTINCT FROM request->>'task_id'
 OR task->>'operation' IS DISTINCT FROM request->>'operation'
 OR task->>'node_key' IS DISTINCT FROM (request->>'operation')||':0'
 OR task->>'params_artifact_id' IS DISTINCT FROM request->>'params_document_id'
 OR task->>'params_sha' IS DISTINCT FROM request->>'params_sha'
 OR task->'execution_profile' IS DISTINCT FROM request->'execution_profile'
 OR plan->'reserved_bytes' IS DISTINCT FROM request->'reserved_bytes' THEN RETURN false;END IF;
 SELECT * INTO params FROM photo.backend_documents d WHERE d.tenant_id=p_tenant AND d.run_id=p_run
 AND d.owner_id=(task->>'id')::uuid AND d.purpose='task_params' AND d.state='ready'
 AND d.artifact_id=(task->>'params_artifact_id')::uuid AND d.sha256=task->>'params_sha'
 AND d.bytes=octet_length(p_proof->>'params_jcs') FOR SHARE;
 IF params.id IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=params.artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=params.sha256 AND a.bytes=params.bytes
 AND a.bucket=params.bucket AND a.object_version=params.object_version AND a.object_version NOT IN('','null')
 AND a.media_type='application/json' AND a.output_role='task_params'
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||params.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a) THEN RETURN false;END IF;
 -- Reconstruct exact typed inventory from the immutable original source.
 SELECT jsonb_agg(item ORDER BY item->'ref'->>'artifact_id') INTO expected FROM (
 SELECT DISTINCT item FROM (
 SELECT jsonb_build_object('contract','Params','ref',jsonb_build_object('artifact_id',request->>'params_document_id','sha256',request->>'params_sha')) AS item
 UNION ALL SELECT jsonb_build_object('contract','WorkingImage','ref',frame->'source') FROM jsonb_array_elements((request->>'params_jcs')::jsonb->'frames') frame
 UNION ALL SELECT jsonb_build_object('contract','Report','ref',request->'execution_profile'->'multi_qualification'->'report')
 UNION ALL SELECT jsonb_build_object('contract','Calibration','ref',request->'execution_profile'->'multi_qualification'->'calibration')
 UNION ALL SELECT jsonb_build_object('contract','NpyArray','ref',(source #>> '{}')::jsonb->'pixels') FROM jsonb_array_elements(request->'sources_jcs') source
 UNION ALL SELECT jsonb_build_object('contract','NpyArray','ref',(source #>> '{}')::jsonb->'alpha') FROM jsonb_array_elements(request->'sources_jcs') source WHERE (source #>> '{}')::jsonb->'alpha' IS NOT NULL AND (source #>> '{}')::jsonb->'alpha'<>'null'::jsonb
 UNION ALL SELECT jsonb_build_object('contract','NpyArray','ref',step->'inverse_field') FROM jsonb_array_elements(request->'sources_jcs') source CROSS JOIN LATERAL jsonb_array_elements((source #>> '{}')::jsonb->'transform_chain'->'steps') step WHERE step->'inverse_field' IS NOT NULL AND step->'inverse_field'<>'null'::jsonb
 ) edges) unique_edges;
 IF entries IS DISTINCT FROM expected OR jsonb_typeof(entries) IS DISTINCT FROM 'array' OR jsonb_array_length(entries) NOT BETWEEN 6 AND 3933 THEN RETURN false;END IF;
 -- Construct the same canonical task digest as Go, rejecting duplicate UUIDs,
 -- unknown fields/contracts and noncanonical ref text before using strings.
 FOR item IN SELECT value FROM jsonb_array_elements(entries) LOOP
 ref:=item->'ref';contract_name:=item->>'contract';
 IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'ref'-'contract')<>'{}'::jsonb
 OR contract_name IS NULL OR contract_name NOT IN('Params','WorkingImage','NpyArray','Report','Calibration')
 OR jsonb_typeof(item->'contract') IS DISTINCT FROM 'string'
 OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'::jsonb
 OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
 OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 aid:=(ref->>'artifact_id')::uuid;
 IF aid='00000000-0000-0000-0000-000000000000'::uuid OR (prior IS NOT NULL AND aid<=prior) THEN RETURN false;END IF;
 IF NOT first_item THEN body:=body||',';END IF;
 body:=body||'{"contract":"'||contract_name||'","ref":{"artifact_id":"'||aid::text||'","sha256":"'||(ref->>'sha256')||'"}}';
 first_item:=false;prior:=aid;
 END LOOP;
 body:=body||']';
 IF encode(sha256(convert_to('{"entries":'||body||',"operation":"'||(task->>'operation')||'","task":"'||(task->>'id')||'","version":"1"}','UTF8')),'hex') IS DISTINCT FROM task->>'input_digest' THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a JOIN jsonb_array_elements(entries) i ON a.id=(i->'ref'->>'artifact_id')::uuid
 WHERE a.tenant_id=p_tenant AND a.sha256=i->'ref'->>'sha256' AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND a.bytes>0
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY a.id FOR SHARE OF a;
 GET DIAGNOSTICS matched=ROW_COUNT;
 IF matched<>jsonb_array_length(entries) THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=(task->>'id')::uuid FOR UPDATE;
 IF old.id IS NOT NULL AND (old.run_id IS DISTINCT FROM p_run OR old.node_key IS DISTINCT FROM task->>'node_key'
 OR old.operation IS DISTINCT FROM task->>'operation' OR old.operation_version IS DISTINCT FROM '1' OR old.resource_class IS DISTINCT FROM 'cpu'
 OR old.params_artifact_id IS DISTINCT FROM params.artifact_id OR old.input_digest IS DISTINCT FROM task->>'input_digest'
 OR old.state IS DISTINCT FROM 'blocked' OR old.attempt IS DISTINCT FROM 0 OR old.cancel_requested IS DISTINCT FROM false
 OR old.execution_profile IS DISTINCT FROM task->'execution_profile') THEN RETURN false;END IF;
 IF r.v29_multi_descriptor_jcs IS NOT NULL AND r.v29_multi_descriptor_jcs IS DISTINCT FROM p_descriptor THEN RETURN false;END IF;
 -- Reserve exact output budget; document transport is accounted separately.
 -- Params storage is already charged and is not charged again.
 needed:=(plan->>'reserved_bytes')::numeric;
 IF needed IS NULL OR needed<1 OR needed>9007199254740991 THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant;
 SELECT storage_bytes::numeric+reserved_bytes INTO used FROM photo.tenant_usage WHERE tenant_id=p_tenant;
 IF capacity IS NULL OR used IS NULL OR used>capacity OR used>9007199254740991 THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run FOR UPDATE;
 IF q.id IS NOT NULL THEN
 IF q.state IS DISTINCT FROM 'reserved' OR q.expires_at<=clock_timestamp() OR q.bytes::numeric<needed
 OR q.principal_type IS DISTINCT FROM 'user' OR q.principal_id IS DISTINCT FROM r.created_by THEN RETURN false;END IF;
 ELSE
 IF used+needed>capacity OR used+needed>9007199254740991 THEN RETURN false;END IF;
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',r.created_by,'run',p_run,needed::bigint,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+needed::bigint WHERE tenant_id=p_tenant;
 END IF;
 IF r.v29_multi_descriptor_jcs IS NULL THEN UPDATE photo.runs SET v29_multi_descriptor_jcs=p_descriptor WHERE tenant_id=p_tenant AND id=p_run;END IF;
 IF old.id IS NULL THEN
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES((task->>'id')::uuid,p_tenant,p_run,task->>'node_key',task->>'operation','1','cpu','blocked','[]'::jsonb,params.artifact_id,task->>'input_digest',task->'execution_profile');
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.stage_v29_multi_task(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v29_multi_task(uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v29_multi_task(uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
-- Generic transport may not bypass the dedicated analysis closure authority.
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) RENAME TO v2_input_document_scope_pre_v29_multi;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope_pre_v29_multi(uuid,uuid,uuid,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v2_input_document_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND config_snapshot ? 'v29_multi_plan') THEN RETURN false;END IF;
 RETURN photo.v2_input_document_scope_pre_v29_multi(p_tenant,p_run,p_task,p_purpose);
END $$;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) RENAME TO register_v2_task_input_batch_pre_v29_multi;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch_pre_v29_multi(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.register_v2_task_input_batch(p_tenant uuid,p_run uuid,p_task uuid,p_first integer,p_rows jsonb) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND config_snapshot ? 'v29_multi_plan') THEN RAISE EXCEPTION 'MULTI_DEDICATED_REGISTRATION_REQUIRED' USING ERRCODE='23514'; END IF;
 RETURN photo.register_v2_task_input_batch_pre_v29_multi(p_tenant,p_run,p_task,p_first,p_rows);
END $$;
ALTER FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v2_task_input_batch(uuid,uuid,uuid,integer,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) RENAME TO seal_v2_task_inputs_pre_v29_multi;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs_pre_v29_multi(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_task_inputs(p_tenant uuid,p_run uuid,p_task uuid,p_manifest jsonb,p_chunks jsonb,p_entry_count integer,p_entries_sha text,p_direct_roots jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND config_snapshot ? 'v29_multi_plan') THEN RETURN false; END IF;
 RETURN photo.seal_v2_task_inputs_pre_v29_multi(p_tenant,p_run,p_task,p_manifest,p_chunks,p_entry_count,p_entries_sha,p_direct_roots);
END $$;
ALTER FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_task_inputs(uuid,uuid,uuid,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v29_multi;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v29_multi(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND config_snapshot ? 'v29_multi_plan') THEN RETURN false; END IF;
 RETURN photo.seal_v2_graph_pre_v29_multi(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
