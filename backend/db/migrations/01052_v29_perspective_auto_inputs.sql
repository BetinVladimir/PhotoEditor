-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.stage_v29_perspective_task(p_tenant uuid,p_run uuid,p_descriptor text,p_proof jsonb)
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
 OR task->>'operation' IS DISTINCT FROM 'optical.perspective' OR task->>'operation_version' IS DISTINCT FROM '1'
 OR task->>'resource_class' IS DISTINCT FROM 'cpu' THEN RETURN false;END IF;
 -- Scope validation takes tenant/usage/source locks before the run lock.
 IF photo.v29_perspective_params_document_scope(p_tenant,p_run,(task->>'id')::uuid,'task_params',task->>'params_sha',octet_length(p_proof->>'params_jcs'),p_proof) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 request:=r.config_snapshot->'v29_perspective_plan';
 IF request IS NULL OR r.kind IS DISTINCT FROM 'v2' OR r.status IS DISTINCT FROM 'draft' OR r.graph_sealed OR r.cancel_requested
 OR task->>'id' IS DISTINCT FROM request->>'task_id'
 OR task->>'operation' IS DISTINCT FROM 'optical.perspective'
 OR task->>'node_key' IS DISTINCT FROM 'optical.perspective:0'
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
 UNION ALL SELECT jsonb_build_object('contract','WorkingImage','ref',(request->>'params_jcs')::jsonb->'base'->'source')
 UNION ALL SELECT jsonb_build_object('contract','Report','ref',request->'execution_profile'->'perspective_qualification'->'report')
 UNION ALL SELECT jsonb_build_object('contract','Calibration','ref',request->'execution_profile'->'perspective_qualification'->'calibration')
 UNION ALL SELECT jsonb_build_object('contract','LineSegments','ref',(request->>'params_jcs')::jsonb->'lines') WHERE (request->>'params_jcs')::jsonb->>'mode'='auto'
 UNION ALL SELECT jsonb_build_object('contract','NpyArray','ref',(request->>'source_jcs')::jsonb->'pixels')
 UNION ALL SELECT jsonb_build_object('contract','NpyArray','ref',(request->>'source_jcs')::jsonb->'alpha') WHERE (request->>'source_jcs')::jsonb->'alpha' IS NOT NULL AND (request->>'source_jcs')::jsonb->'alpha'<>'null'::jsonb
 UNION ALL SELECT jsonb_build_object('contract','NpyArray','ref',value->'inverse_field') FROM jsonb_array_elements((request->>'source_jcs')::jsonb->'transform_chain'->'steps') WHERE value->'inverse_field' IS NOT NULL AND value->'inverse_field'<>'null'::jsonb
 ) edges) unique_edges;
 IF entries IS DISTINCT FROM expected OR jsonb_typeof(entries) IS DISTINCT FROM 'array' OR jsonb_array_length(entries) NOT BETWEEN 5 AND 135 THEN RETURN false;END IF;
 -- Construct the same canonical task digest as Go, rejecting duplicate UUIDs,
 -- unknown fields/contracts and noncanonical ref text before using strings.
 FOR item IN SELECT value FROM jsonb_array_elements(entries) LOOP
 ref:=item->'ref';contract_name:=item->>'contract';
 IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'ref'-'contract')<>'{}'::jsonb
 OR contract_name IS NULL OR contract_name NOT IN('Params','WorkingImage','NpyArray','Report','Calibration','LineSegments')
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
 OR (old.execution_profile IS DISTINCT FROM task->'execution_profile' AND NOT(photo.v2_input_closure_ready(old.id) AND (old.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=task->'execution_profile'))) THEN RETURN false;END IF;
 IF r.v29_perspective_descriptor_jcs IS NOT NULL AND r.v29_perspective_descriptor_jcs IS DISTINCT FROM p_descriptor THEN RETURN false;END IF;
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
 IF r.v29_perspective_descriptor_jcs IS NULL THEN UPDATE photo.runs SET v29_perspective_descriptor_jcs=p_descriptor WHERE tenant_id=p_tenant AND id=p_run;END IF;
 IF old.id IS NULL THEN
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES((task->>'id')::uuid,p_tenant,p_run,task->>'node_key',task->>'operation','1','cpu','blocked','[]'::jsonb,params.artifact_id,task->>'input_digest',task->'execution_profile');
 END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.stage_v29_perspective_task(uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.stage_v29_perspective_task(uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.stage_v29_perspective_task(uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.register_v29_perspective_transport(p_tenant uuid,p_run uuid,p_descriptor text,p_proof jsonb,p_transport jsonb,p_manifest_body text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE plan jsonb;request jsonb;entries jsonb;task uuid;n integer;position integer;batch jsonb;matched integer;
 d photo.backend_documents%ROWTYPE;body jsonb;roots jsonb;actual jsonb;expected jsonb;chunk jsonb;counter integer:=0;
BEGIN
 IF jsonb_typeof(p_transport) IS DISTINCT FROM 'object'
 OR (SELECT array_agg(k ORDER BY k) FROM jsonb_object_keys(p_transport) k) IS DISTINCT FROM ARRAY['chunks','count','manifest','sha256']::text[]
 OR photo.stage_v29_perspective_task(p_tenant,p_run,p_descriptor,p_proof) IS DISTINCT FROM true THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;entries:=plan->'entries';task:=(plan->'task'->>'id')::uuid;n:=jsonb_array_length(entries);
 SELECT config_snapshot->'v29_perspective_plan' INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 body:=p_manifest_body::jsonb;
 IF p_transport->'count' IS DISTINCT FROM to_jsonb(n) OR p_transport->'chunks' IS DISTINCT FROM body->'chunks'
 OR p_transport->>'sha256' IS DISTINCT FROM body->>'entries_sha'
 OR photo.v29_perspective_transport_document_scope(p_tenant,p_run,task,'v2_input_manifest',
 p_transport->'manifest'->>'sha256',octet_length(p_manifest_body),p_proof,p_manifest_body) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run AND owner_id=task AND purpose='v2_input_manifest'
 AND state='ready' AND artifact_id=(p_transport->'manifest'->>'artifact_id')::uuid AND sha256=p_transport->'manifest'->>'sha256' FOR SHARE;
 IF d.id IS NULL OR d.bytes IS DISTINCT FROM octet_length(p_manifest_body)::bigint
 OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket
 AND a.object_version=d.object_version AND a.object_version NOT IN('','null') AND a.media_type='application/json' AND a.output_role=d.purpose
 AND a.object_key='tenants/'||p_tenant||'/runs/'||p_run||'/documents/'||d.artifact_id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a) THEN RETURN false;END IF;
 -- Only Params/source/report are direct wire roots. Source arrays remain
 -- in the complete frozen closure and registered task inputs.
 SELECT jsonb_agg(ref ORDER BY ref->>'artifact_id') INTO roots FROM (
 SELECT jsonb_build_object('artifact_id',plan->'task'->>'params_artifact_id','sha256',plan->'task'->>'params_sha') ref
 UNION ALL SELECT (request->>'params_jcs')::jsonb->'base'->'source'
 UNION ALL SELECT request->'execution_profile'->'perspective_qualification'->'report'
 UNION ALL SELECT request->'execution_profile'->'perspective_qualification'->'calibration'
 UNION ALL SELECT (request->>'params_jcs')::jsonb->'lines' WHERE (request->>'params_jcs')::jsonb->>'mode'='auto') refs;
 expected:=entries;
 FOR chunk IN SELECT value FROM jsonb_array_elements(p_transport->'chunks') LOOP
 expected:=expected||jsonb_build_array(jsonb_build_object('contract','InputClosureChunk','ref',chunk));counter:=counter+1;
 END LOOP;
 expected:=expected||jsonb_build_array(jsonb_build_object('contract','InputClosureManifest','ref',p_transport->'manifest'));
 SELECT jsonb_agg(jsonb_build_object('contract',i.contract,'ref',jsonb_build_object('artifact_id',i.artifact_id,'sha256',i.sha256)) ORDER BY i.ordinal)
 INTO actual FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=task;
 IF photo.v2_input_closure_ready(task) THEN
 RETURN actual=expected AND EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=task
 AND t.inputs=jsonb_build_array(p_transport->'manifest')||roots
 AND t.execution_profile->'v2_input_manifest'=p_transport->'manifest'
 AND t.execution_profile->'v2_entry_count'=to_jsonb(n) AND t.execution_profile->>'v2_entries_sha'=p_transport->>'sha256');END IF;
 IF actual IS NOT NULL THEN RETURN false;END IF;
 position:=0;
 WHILE position<n LOOP
 SELECT jsonb_agg(value ORDER BY ord) INTO batch FROM jsonb_array_elements(entries) WITH ORDINALITY a(value,ord) WHERE ord>position AND ord<=position+512;
 matched:=photo.register_v2_task_input_batch_pre_relight(p_tenant,p_run,task,position,batch);
 IF matched IS DISTINCT FROM jsonb_array_length(batch) THEN RAISE EXCEPTION 'PERSPECTIVE_BATCH_CHANGED' USING ERRCODE='23514';END IF;
 position:=position+matched;
 END LOOP;
 IF photo.seal_v2_task_inputs_pre_relight(p_tenant,p_run,task,p_transport->'manifest',p_transport->'chunks',n,p_transport->>'sha256',roots) IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'PERSPECTIVE_INPUT_SEAL_FAILED' USING ERRCODE='23514';END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_v29_perspective_transport(uuid,uuid,text,jsonb,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_v29_perspective_transport(uuid,uuid,text,jsonb,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_v29_perspective_transport(uuid,uuid,text,jsonb,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.lock_v29_perspective_recovery_source(p_tenant uuid,p_plan jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE params jsonb:=(p_plan->>'params_jcs')::jsonb;image jsonb:=(p_plan->>'source_jcs')::jsonb;edge jsonb;i integer;role text;
BEGIN
 IF encode(sha256(convert_to(p_plan->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM params->'base'->'source'->>'sha256' THEN RETURN false;END IF;
 FOR role,edge IN SELECT 'WorkingPixels',image->'pixels' UNION ALL SELECT 'WorkingAlpha',image->'alpha' LOOP
 IF edge IS NULL OR edge='null'::jsonb THEN IF role='WorkingPixels' THEN RETURN false;END IF;CONTINUE;END IF;
 IF photo.lock_v29_perspective_source_child(p_tenant,(p_plan->>'project_id')::uuid,(p_plan->>'snapshot_id')::uuid,(p_plan->>'actor_id')::uuid,(p_plan->>'auth_session_id')::uuid,params->'base',p_plan->>'source_jcs',(edge->>'artifact_id')::uuid,edge->>'sha256',role) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 IF jsonb_typeof(image->'transform_chain'->'steps') IS DISTINCT FROM 'array' OR jsonb_array_length(image->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
 FOR edge,i IN SELECT value,(ordinality-1)::integer FROM jsonb_array_elements(image->'transform_chain'->'steps') WITH ORDINALITY LOOP
 IF edge->>'kind'='dense_warp' AND photo.lock_v29_perspective_source_child(p_tenant,(p_plan->>'project_id')::uuid,(p_plan->>'snapshot_id')::uuid,(p_plan->>'actor_id')::uuid,(p_plan->>'auth_session_id')::uuid,params->'base',p_plan->>'source_jcs',(edge->'inverse_field'->>'artifact_id')::uuid,edge->'inverse_field'->>'sha256','InverseField/'||i) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 IF params->>'mode'='auto' AND (encode(sha256(convert_to(p_plan->>'lines_jcs','UTF8')),'hex') IS DISTINCT FROM params->'lines'->>'sha256' OR photo.v29_perspective_lines_document(p_tenant,(p_plan->>'project_id')::uuid,(p_plan->>'snapshot_id')::uuid,(p_plan->>'actor_id')::uuid,(p_plan->>'auth_session_id')::uuid,params->'base',(params->'lines'->>'artifact_id')::uuid,params->'lines'->>'sha256') IS NULL) THEN RETURN false;END IF;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_perspective_recovery_source(uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_perspective_recovery_source(uuid,jsonb) FROM PUBLIC;
-- Invoked only by the SECURITY DEFINER receipt reader.
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
