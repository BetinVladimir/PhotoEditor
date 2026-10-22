-- +goose Up
-- Accepted-master parent authority is separate from a candidate with matching
-- pixels. The exact master producer generation owns the optional ImageResult.
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_source_parent(p_tenant uuid,p_project uuid,p_version uuid,p_source uuid,p_sha text,p_actor uuid,p_session uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text,closure jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source photo.artifacts%ROWTYPE;producer photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;root photo.artifacts%ROWTYPE;roots integer;refs jsonb;
BEGIN
 IF photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true THEN RETURN;END IF;
 SELECT a.* INTO source FROM photo.asset_versions v JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE v.tenant_id=p_tenant AND v.id=p_version AND v.master_artifact_id=p_source AND v.content_hash=p_sha AND v.review_state='accepted'
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF source.id IS NULL THEN RAISE EXCEPTION 'EDITOR_SOURCE_PARENT_AUTHORITY' USING ERRCODE='42501';END IF;
 IF source.created_by_task_id IS NULL THEN RETURN;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=source.created_by_task_id;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id AND task_id=producer.id;
 IF producer.id IS NULL OR result.id IS NULL OR producer.state<>'succeeded' OR producer.cancel_requested
 OR producer.attempt IS DISTINCT FROM source.produced_attempt OR producer.fencing_token IS DISTINCT FROM source.produced_fence
 OR result.attempt IS DISTINCT FROM producer.attempt OR result.fencing_token IS DISTINCT FROM producer.fencing_token
 OR NOT result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',p_source,'sha256',p_sha)) THEN
 RAISE EXCEPTION 'EDITOR_SOURCE_PARENT_GENERATION' USING ERRCODE='23514';END IF;
 SELECT count(*) INTO roots FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=producer.id
 AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.output_role IN('document','image_result')
 AND a.media_type='application/json' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF roots=0 THEN
  IF EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=producer.run_id AND t.operation IN('image.generate','image.inpaint','image.outpaint')) THEN
   RAISE EXCEPTION 'EDITOR_SOURCE_GENERATED_PROVENANCE_UNAVAILABLE' USING ERRCODE='23514';END IF;
  RETURN;
 END IF;
 IF roots<>1 THEN RAISE EXCEPTION 'EDITOR_SOURCE_PARENT_AMBIGUOUS' USING ERRCODE='23514';END IF;
 SELECT a.* INTO root FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.created_by_task_id=producer.id
 AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.output_role IN('document','image_result')
 AND a.media_type='application/json' AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
 IF root.state<>'ready' OR root.verified_at IS NULL OR root.object_version IS NULL OR root.object_version IN('','null')
 OR root.bytes NOT BETWEEN 2 AND 1048576 OR root.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'EDITOR_SOURCE_PARENT_NOT_READY' USING ERRCODE='23514';END IF;
 SELECT coalesce(jsonb_agg(ref ORDER BY ref->>'artifact_id'),'[]'::jsonb) INTO refs FROM (
 SELECT DISTINCT jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ref FROM (
 SELECT value FROM jsonb_array_elements(coalesce(producer.inputs,'[]'::jsonb)) UNION ALL SELECT value FROM jsonb_array_elements(result.outputs)) declared
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(declared.value->>'artifact_id')::uuid AND a.sha256=declared.value->>'sha256'
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) checked;
 IF jsonb_array_length(refs)>4096 THEN RAISE EXCEPTION 'EDITOR_SOURCE_PARENT_CLOSURE_LIMIT' USING ERRCODE='23514';END IF;
 RETURN QUERY SELECT root.id,root.sha256,root.bucket,root.object_key,root.object_version,root.bytes,root.media_type,refs;
END $$;
ALTER FUNCTION photo.editor_effect_source_parent(uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_source_parent(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_source_parent(uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 RENAME TO editor_layers_precompose_worker_origin_ready;
REVOKE ALL ON FUNCTION photo.editor_layers_precompose_worker_origin_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM photo_worker;
CREATE FUNCTION photo.editor_layers_precompose_worker_ready(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;parent jsonb;
BEGIN
 IF photo.editor_layers_precompose_worker_origin_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_run FOR SHARE;
 SELECT jsonb_build_object('artifact_id',p.artifact_id,'sha256',p.sha256) INTO parent
 FROM photo.editor_effect_source_parent(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.actor_id,b.session_id) p;
 RETURN coalesce(parent,'null'::jsonb)=coalesce(p_json::jsonb->'source_result','null'::jsonb);
EXCEPTION WHEN invalid_text_representation OR check_violation OR insufficient_privilege THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_layers_precompose_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose StatementBegin
-- Discovery is not admission: enqueue repeats canonical fences under locks.
CREATE FUNCTION photo.editor_effect_seal_context(p_after uuid)
RETURNS TABLE(tenant_id uuid,run_id uuid,origin jsonb,coverage_trace jsonb,artifacts jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;r photo.runs%ROWTYPE;cursor_ref jsonb;initial_ref jsonb;
 trace jsonb:='[]'::jsonb;entry jsonb;t photo.tasks%ROWTYPE;z photo.task_results%ROWTYPE;n photo.apply_nodes%ROWTYPE;
 output photo.artifacts%ROWTYPE;report photo.artifacts%ROWTYPE;inventory jsonb;seen uuid[]:='{}';refs jsonb;
BEGIN
 SELECT e.* INTO b FROM photo.editor_effect_builds e WHERE e.state IN('effects_ready','sealing') AND (p_after IS NULL OR e.run_id>p_after)
 AND photo.v2_editor_authorized(e.tenant_id,e.project_id,e.actor_id,e.session_id)
 AND (e.state='effects_ready' OR EXISTS(SELECT FROM photo.runs seal WHERE seal.tenant_id=e.tenant_id AND seal.id=e.seal_run_id AND seal.status='draft' AND NOT seal.cancel_requested))
 ORDER BY e.run_id LIMIT 1;
 IF b.run_id IS NULL THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE runs.tenant_id=b.tenant_id AND id=b.run_id;
 IF r.cancel_requested OR r.status<>'completed' OR NOT photo.editor_effect_origin_scope(b.tenant_id,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id) THEN RETURN;END IF;
 initial_ref:=jsonb_build_object('artifact_id',coalesce(b.effect_source_artifact_id,b.source_artifact_id),'sha256',coalesce(b.effect_source_sha,b.source_sha));
 cursor_ref:=jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha);
 WHILE cursor_ref IS DISTINCT FROM initial_ref LOOP
  IF jsonb_array_length(trace)>=128 THEN RAISE EXCEPTION 'EDITOR_TRACE_LIMIT' USING ERRCODE='23514';END IF;
  SELECT a.* INTO output FROM photo.artifacts a WHERE a.tenant_id=b.tenant_id AND a.id=(cursor_ref->>'artifact_id')::uuid AND a.sha256=cursor_ref->>'sha256';
  SELECT task.* INTO t FROM photo.tasks task WHERE task.tenant_id=b.tenant_id AND task.run_id=b.run_id AND task.id=output.created_by_task_id;
  SELECT result.* INTO z FROM photo.task_results result WHERE result.tenant_id=b.tenant_id AND result.id=t.canonical_result_id AND result.task_id=t.id;
  SELECT node.* INTO n FROM photo.apply_nodes node WHERE node.tenant_id=b.tenant_id AND node.run_id=b.run_id AND node.id=t.id;
  IF output.id IS NULL OR t.id IS NULL OR z.id IS NULL OR n.id IS NULL OR t.id=ANY(seen) OR t.state<>'succeeded' OR t.cancel_requested
   OR output.output_role<>'working_image' OR output.state<>'ready' OR output.verified_at IS NULL
   OR output.produced_attempt IS DISTINCT FROM t.attempt OR output.produced_fence IS DISTINCT FROM t.fencing_token
   OR z.attempt IS DISTINCT FROM t.attempt OR z.fencing_token IS DISTINCT FROM t.fencing_token
   OR NOT z.outputs @> jsonb_build_array(cursor_ref) OR jsonb_typeof(n.params->'image') IS DISTINCT FROM 'object'
   OR NOT t.inputs @> jsonb_build_array(n.params->'image') THEN RAISE EXCEPTION 'EDITOR_TRACE_CANONICAL_CHAIN' USING ERRCODE='23514';END IF;
  seen:=array_append(seen,t.id);report:=NULL;
  IF t.operation IN('image.generate','image.inpaint','image.outpaint') THEN
   SELECT a.* INTO STRICT report FROM photo.artifacts a WHERE a.tenant_id=b.tenant_id AND a.created_by_task_id=t.id
    AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
    AND a.output_role=CASE WHEN t.operation='image.inpaint' THEN 'inpaint_report' ELSE 'generation_report' END
    AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256));
  END IF;
  entry:=jsonb_build_object('task_id',t.id,'result_id',z.id,'attempt',t.attempt,'fence',t.fencing_token,'operation',t.operation,
   'source',n.params->'image','output',cursor_ref,'report',CASE WHEN report.id IS NULL THEN 'null'::jsonb ELSE jsonb_build_object('artifact_id',report.id,'sha256',report.sha256) END);
  trace:=jsonb_build_array(entry)||trace;cursor_ref:=n.params->'image';
 END LOOP;
 refs:=coalesce(r.config_snapshot#>'{editor_effect_source,inputs}','[]'::jsonb)
  ||jsonb_build_array(jsonb_build_object('artifact_id',b.executed_style_artifact_id,'sha256',b.executed_style_sha),jsonb_build_object('artifact_id',b.quality_artifact_id,'sha256',b.quality_sha));
 SELECT refs||coalesce(jsonb_agg(declared.value),'[]'::jsonb) INTO refs FROM photo.tasks task
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=task.canonical_result_id AND result.task_id=task.id
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(task.inputs,'[]'::jsonb)||result.outputs) declared
 WHERE task.tenant_id=b.tenant_id AND task.run_id=b.run_id AND task.state='succeeded' AND NOT task.cancel_requested
 AND result.attempt=task.attempt AND result.fencing_token=task.fencing_token;
 SELECT jsonb_agg(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version_id',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY a.id) INTO inventory
 FROM photo.artifacts a JOIN (SELECT DISTINCT value FROM jsonb_array_elements(refs)) declared
 ON a.tenant_id=b.tenant_id AND a.id=(declared.value->>'artifact_id')::uuid AND a.sha256=declared.value->>'sha256'
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
 IF jsonb_array_length(coalesce(inventory,'[]'::jsonb))>4096 THEN RAISE EXCEPTION 'EDITOR_SEAL_CLOSURE_LIMIT' USING ERRCODE='23514';END IF;
 RETURN QUERY SELECT b.tenant_id,b.run_id,to_jsonb(b)||jsonb_build_object('editor_source',r.config_snapshot->'editor_effect_source','inherited_review',EXISTS(SELECT FROM photo.reviews review WHERE review.tenant_id=b.tenant_id AND review.run_id=b.run_id)),trace,inventory;
END $$;
ALTER FUNCTION photo.editor_effect_seal_context(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_context(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_context(uuid) TO photo_scheduler;
-- +goose StatementEnd
ALTER TABLE photo.editor_effect_builds ADD COLUMN seal_plan_params text,ADD COLUMN seal_plan_inputs jsonb,ADD COLUMN seal_plan_profile jsonb;
ALTER TABLE photo.backend_documents ADD COLUMN editor_effects_params text;
ALTER TABLE photo.backend_documents ADD CONSTRAINT backend_documents_editor_effects_body CHECK(editor_effects_params IS NULL OR
 (purpose='task_params' AND octet_length(editor_effects_params)=bytes AND encode(sha256(convert_to(editor_effects_params,'UTF8')),'hex')=sha256));
-- +goose StatementBegin
CREATE FUNCTION photo.prepare_editor_effect_seal(p_tenant uuid,p_build uuid,p_run uuid,p_task uuid,p_params text,p_inputs jsonb,p_profile jsonb)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;r photo.runs%ROWTYPE;body jsonb;tenant photo.tenants%ROWTYPE;usage photo.tenant_usage%ROWTYPE;
 h bytea;expected uuid;cost bigint;parent jsonb;config jsonb;
BEGIN
 IF p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 1048576 OR jsonb_typeof(p_inputs) IS DISTINCT FROM 'array'
 OR jsonb_array_length(p_inputs) NOT BETWEEN 1 AND 4096 OR p_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR p_profile#>'{resource_estimate,vram_bytes}' IS DISTINCT FROM '0'::jsonb THEN RETURN NULL;END IF;
 body:=p_params::jsonb;
 SELECT * INTO tenant FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN NULL;END IF;
 PERFORM run_id FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state NOT IN('effects_ready','sealing') OR r.status<>'completed' OR r.cancel_requested
 OR NOT photo.editor_effect_origin_scope(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id)
 OR NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=b.producer_task_id AND t.run_id=p_build AND t.state='succeeded'
 AND NOT t.cancel_requested AND t.canonical_result_id=b.producer_result_id AND t.attempt=b.producer_attempt AND t.fencing_token=b.producer_fence)
 OR NOT EXISTS(SELECT FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=b.quality_task_id AND t.run_id=p_build AND t.state='succeeded'
 AND NOT t.cancel_requested AND t.canonical_result_id=b.quality_result_id AND t.operation='quality.check') THEN RETURN NULL;END IF;
 SELECT jsonb_build_object('artifact_id',p.artifact_id,'sha256',p.sha256) INTO parent FROM photo.editor_effect_source_parent(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.actor_id,b.session_id) p;
 IF coalesce(parent,'null'::jsonb) IS DISTINCT FROM coalesce(r.config_snapshot#>'{editor_effect_source,source_result}','null'::jsonb) THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(p_build)||convert_to('editor.effects_seal/run','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM p_run THEN RETURN NULL;END IF;
 h:=public.digest(uuid_send(p_run)||convert_to('editor.effects_seal','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;
 IF expected IS DISTINCT FROM p_task THEN RETURN NULL;END IF;
 IF body->'schema_version' IS DISTINCT FROM '1'::jsonb OR body->>'build_run_id' IS DISTINCT FROM p_build::text
 OR body->'original_recipe' IS DISTINCT FROM jsonb_build_object('artifact_id',b.frozen_recipe_artifact_id,'sha256',b.frozen_recipe_sha)
 OR body->'original_source' IS DISTINCT FROM jsonb_build_object('asset_version_id',b.asset_version_id,'source',jsonb_build_object('artifact_id',b.source_artifact_id,'sha256',b.source_sha),'canvas_hash',b.source_canvas_hash)
 OR body#>'{processed_source,source}' IS DISTINCT FROM jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha)
 OR body#>>'{processed_source,asset_version_id}' IS DISTINCT FROM b.asset_version_id::text
 OR coalesce(body#>>'{processed_source,canvas_hash}','') !~ '^[0-9a-f]{64}$'
 OR body->'quality' IS DISTINCT FROM jsonb_build_object('artifact_id',b.quality_artifact_id,'sha256',b.quality_sha)
 OR body->'executed_style' IS DISTINCT FROM jsonb_build_object('artifact_id',b.executed_style_artifact_id,'sha256',b.executed_style_sha)
 OR body->>'compilation_sha' IS DISTINCT FROM b.compilation_sha OR coalesce(body->>'trace_sha','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(body->'generative_requested') IS DISTINCT FROM 'boolean' OR jsonb_typeof(body->'inherited_review') IS DISTINCT FROM 'boolean'
 OR jsonb_array_length(coalesce(body->'coverage_trace','[]'::jsonb))>128 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT FROM jsonb_array_elements(p_inputs) i LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(i->>'artifact_id')::uuid AND a.sha256=i->>'sha256'
 WHERE a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp())
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_inputs))<>jsonb_array_length(p_inputs)
 OR NOT p_inputs @> jsonb_build_array(body->'original_recipe',body#>'{original_source,source}',body#>'{processed_source,source}',body->'executed_style',body->'quality') THEN RETURN NULL;END IF;
 IF b.state='sealing' THEN
  IF b.seal_run_id=p_run AND b.seal_task_id=p_task AND b.seal_plan_params=p_params AND b.seal_plan_inputs=p_inputs AND b.seal_plan_profile=p_profile
   AND EXISTS(SELECT FROM photo.runs seal WHERE seal.tenant_id=p_tenant AND seal.id=p_run AND NOT seal.cancel_requested AND seal.status IN('draft','running')) THEN RETURN p_run;END IF;RETURN NULL;
 END IF;
 cost:=(p_profile#>>'{resource_estimate,output_bytes}')::bigint;
 IF cost IS NULL OR cost NOT BETWEEN 1 AND 9007199254740991 OR usage.storage_bytes::numeric+usage.reserved_bytes+cost>(tenant.quotas->>'storage_bytes')::numeric
 OR usage.storage_bytes::numeric+usage.reserved_bytes+cost>9007199254740991 OR tenant.quotas->>'storage_bytes' IS NULL
 OR NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=p_build AND q.state='reserved' AND q.expires_at>clock_timestamp()) THEN RETURN NULL;END IF;
 config:=jsonb_build_object('editor_effect_build',p_build,'project_id',b.project_id,'auth_user_id',b.actor_id,'auth_session_id',b.session_id,'params_sha',encode(sha256(convert_to(p_params,'UTF8')),'hex'));
 INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed,failure_policy)
 VALUES(p_run,p_tenant,'v2','draft',r.input_snapshot_id,config,encode(sha256(convert_to(config::text,'UTF8')),'hex'),b.actor_id,false,'fail_fast');
 INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
 VALUES(p_tenant,b.project_id,p_run,r.input_snapshot_id,'provenance','v2.2',b.compilation_sha);
 INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,expires_at)
 VALUES(p_tenant,'user',b.actor_id,'run',p_run,cost,0,clock_timestamp()+interval '1 hour');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+cost WHERE tenant_id=p_tenant;
 UPDATE photo.editor_effect_builds SET state='sealing',seal_run_id=p_run,seal_task_id=p_task,seal_plan_params=p_params,seal_plan_inputs=p_inputs,seal_plan_profile=p_profile,
 processed_source_canvas_hash=body#>>'{processed_source,canvas_hash}',seal_trace_sha=body->>'trace_sha',seal_coverage_trace=coalesce(body->'coverage_trace','[]'::jsonb),
 seal_generated_mask=nullif(body->'generated','null'::jsonb),seal_generative_requested=(body->>'generative_requested')::boolean,seal_inherited_review=(body->>'inherited_review')::boolean WHERE tenant_id=p_tenant AND run_id=p_build;
 RETURN p_run;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR check_violation OR insufficient_privilege THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.prepare_editor_effect_seal(uuid,uuid,uuid,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.prepare_editor_effect_seal(uuid,uuid,uuid,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.prepare_editor_effect_seal(uuid,uuid,uuid,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.reserve_editor_effect_seal_document(p_tenant uuid,p_build uuid,p_bucket text)
RETURNS TABLE(artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;d photo.backend_documents%ROWTYPE;size bigint;sha text;usage photo.tenant_usage%ROWTYPE;capacity numeric;
BEGIN
 IF length(coalesce(p_bucket,'')) NOT BETWEEN 3 AND 63 THEN RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state<>'sealing' OR b.seal_plan_params IS NULL
 OR NOT photo.editor_effect_origin_scope(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id)
 OR NOT EXISTS(SELECT FROM photo.runs WHERE tenant_id=p_tenant AND id=b.seal_run_id AND status='draft' AND NOT graph_sealed AND NOT cancel_requested)
 OR NOT EXISTS(SELECT FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=b.seal_run_id AND state='reserved' AND expires_at>clock_timestamp()) THEN RETURN;END IF;
 sha:=encode(sha256(convert_to(b.seal_plan_params,'UTF8')),'hex');size:=octet_length(b.seal_plan_params);
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=b.seal_run_id AND owner_id=b.seal_task_id AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN
  IF d.sha256=sha AND d.bytes=size AND d.bucket=p_bucket AND d.editor_effects_params=b.seal_plan_params AND d.state IN('pending','ready')
   AND (d.state='pending' OR EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256 AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_key='tenants/'||p_tenant||'/runs/'||b.seal_run_id||'/documents/'||d.artifact_id AND a.output_role='task_params' AND a.media_type='application/json' AND a.state='ready' AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN QUERY SELECT d.artifact_id,d.token,d.state='ready';END IF;RETURN;
 END IF;
 IF capacity IS NULL OR usage.tenant_id IS NULL OR usage.storage_bytes::numeric+usage.reserved_bytes+size>capacity OR usage.storage_bytes::numeric+usage.reserved_bytes+size>9007199254740991 THEN RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,editor_effects_params)
 VALUES(p_tenant,b.seal_run_id,b.seal_task_id,'task_params',gen_random_uuid(),gen_random_uuid(),sha,size,p_bucket,b.seal_plan_params) RETURNING * INTO d;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 UPDATE photo.editor_effect_builds SET seal_params_artifact_id=d.artifact_id,seal_params_sha=sha WHERE tenant_id=p_tenant AND run_id=p_build;
 RETURN QUERY SELECT d.artifact_id,d.token,false;
END $$;
ALTER FUNCTION photo.reserve_editor_effect_seal_document(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_editor_effect_seal_document(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.reserve_editor_effect_seal_document(uuid,uuid,text) TO photo_scheduler;
CREATE FUNCTION photo.complete_editor_effect_seal_document(p_tenant uuid,p_build uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;d photo.backend_documents%ROWTYPE;target text;
BEGIN
 IF p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state<>'sealing' OR NOT photo.editor_effect_origin_scope(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id)
 OR NOT EXISTS(SELECT FROM photo.runs WHERE tenant_id=p_tenant AND id=b.seal_run_id AND status='draft' AND NOT graph_sealed AND NOT cancel_requested) THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=b.seal_run_id AND owner_id=b.seal_task_id AND purpose='task_params' FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha
 OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket OR d.editor_effects_params IS DISTINCT FROM b.seal_plan_params THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||b.seal_run_id||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_artifact AND sha256=p_sha AND bytes=p_bytes AND state='ready' AND object_version=p_version AND bucket=p_bucket AND object_key=target AND verified_at IS NOT NULL);END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'EDITOR_SEAL_DOCUMENT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_editor_effect_seal_document(uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_editor_effect_seal_document(uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.complete_editor_effect_seal_document(uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.materialize_editor_effect_seal(p_tenant uuid,p_build uuid,p_task jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;d photo.backend_documents%ROWTYPE;old photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;ref jsonb;refs_text text:='';digest text;parent jsonb;original photo.runs%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM run_id FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 SELECT * INTO original FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state<>'sealing' OR original.cancel_requested
 OR NOT photo.editor_effect_origin_scope(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id) THEN RETURN false;END IF;
 SELECT jsonb_build_object('artifact_id',p.artifact_id,'sha256',p.sha256) INTO parent FROM photo.editor_effect_source_parent(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.actor_id,b.session_id) p;
 IF coalesce(parent,'null'::jsonb) IS DISTINCT FROM coalesce(original.config_snapshot#>'{editor_effect_source,source_result}','null'::jsonb) THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=b.seal_run_id FOR UPDATE;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=b.seal_run_id AND owner_id=b.seal_task_id AND purpose='task_params' FOR SHARE;
 IF r.id IS NULL OR r.cancel_requested OR r.status NOT IN('draft','running') OR d.id IS NULL OR d.state<>'ready'
 OR d.artifact_id IS DISTINCT FROM b.seal_params_artifact_id OR d.sha256 IS DISTINCT FROM b.seal_params_sha
 OR d.editor_effects_params IS DISTINCT FROM b.seal_plan_params
 OR p_task->>'id' IS DISTINCT FROM b.seal_task_id::text OR p_task->>'node_key' IS DISTINCT FROM 'editor.effects_seal'
 OR p_task->>'operation' IS DISTINCT FROM 'editor.effects_seal' OR p_task->>'operation_version' IS DISTINCT FROM '1'
 OR p_task->>'resource_class' IS DISTINCT FROM 'cpu' OR p_task->'timeout_seconds' IS DISTINCT FROM '300'::jsonb
 OR p_task->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',d.artifact_id,'sha256',d.sha256)
 OR p_task->'inputs' IS DISTINCT FROM b.seal_plan_inputs OR p_task->'execution_profile' IS DISTINCT FROM b.seal_plan_profile
 OR (p_task-'id'-'node_key'-'operation'-'operation_version'-'resource_class'-'timeout_seconds'-'params'-'inputs'-'input_digest'-'execution_profile')<>'{}'::jsonb
 OR NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant AND q.holder_type='run' AND q.holder_id=b.seal_run_id AND q.state='reserved' AND q.expires_at>clock_timestamp()
 AND q.bytes>=(b.seal_plan_profile#>>'{resource_estimate,output_bytes}')::bigint)
 OR EXISTS(SELECT FROM jsonb_array_elements(b.seal_plan_inputs) i LEFT JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(i->>'artifact_id')::uuid AND a.sha256=i->>'sha256'
 WHERE a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR a.expires_at<=clock_timestamp()) THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_array_elements(b.seal_plan_inputs) LOOP
  IF refs_text<>'' THEN refs_text:=refs_text||',';END IF;
  refs_text:=refs_text||format('{"artifact_id":"%s","sha256":"%s"}',(ref->>'artifact_id')::uuid,ref->>'sha256');
 END LOOP;
 digest:=encode(sha256(convert_to(format('{"inputs":[%s],"operation":"editor.effects_seal","operation_version":"1","params":{"artifact_id":"%s","sha256":"%s"},"version":"task_input_v1"}',refs_text,d.artifact_id,d.sha256),'UTF8')),'hex');
 IF p_task->>'input_digest' IS DISTINCT FROM digest THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.tasks WHERE tenant_id=p_tenant AND id=b.seal_task_id FOR UPDATE;
 IF FOUND THEN RETURN old.run_id=b.seal_run_id AND old.operation='editor.effects_seal' AND old.operation_version='1' AND old.inputs=b.seal_plan_inputs
 AND old.params_artifact_id=d.artifact_id AND old.input_digest=digest AND old.execution_profile=b.seal_plan_profile AND NOT old.cancel_requested;END IF;
 IF r.status<>'draft' OR r.graph_sealed THEN RETURN false;END IF;
 INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile)
 VALUES(b.seal_task_id,p_tenant,b.seal_run_id,'editor.effects_seal','editor.effects_seal','1','cpu','ready',b.seal_plan_inputs,d.artifact_id,digest,b.seal_plan_profile);
 UPDATE photo.runs SET status='running',graph_sealed=true,revision=revision+1 WHERE tenant_id=p_tenant AND id=b.seal_run_id;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR check_violation OR insufficient_privilege THEN RETURN false;
END $$;
ALTER FUNCTION photo.materialize_editor_effect_seal(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.materialize_editor_effect_seal(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.materialize_editor_effect_seal(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_source_parent_current(p_tenant uuid,p_build uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;origin photo.runs%ROWTYPE;parent jsonb;
BEGIN
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build;
 SELECT * INTO origin FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build;
 IF b.run_id IS NULL OR origin.id IS NULL OR origin.cancel_requested THEN RETURN false;END IF;
 SELECT jsonb_build_object('artifact_id',p.artifact_id,'sha256',p.sha256) INTO parent FROM photo.editor_effect_source_parent(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.actor_id,b.session_id) p;
 RETURN coalesce(parent,'null'::jsonb)=coalesce(origin.config_snapshot#>'{editor_effect_source,source_result}','null'::jsonb);
EXCEPTION WHEN invalid_text_representation OR check_violation OR insufficient_privilege THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_effect_source_parent_current(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_source_parent_current(uuid,uuid) FROM PUBLIC;
DO $$ DECLARE source text;needle text:='AND t.params_artifact_id=b.seal_params_artifact_id AND photo.v2_editor_authorized(p_tenant,b.project_id,b.actor_id,b.session_id)';BEGIN
 SELECT pg_get_functiondef('photo.editor_effect_seal_adoption_scope(uuid,uuid,integer,bigint,uuid)'::regprocedure) INTO source;
 IF position(needle IN source)=0 THEN RAISE EXCEPTION 'EDITOR_SEAL_ADOPTION_PARENT_PATCH_SHAPE';END IF;
 EXECUTE replace(source,needle,needle||' AND photo.editor_effect_source_parent_current(p_tenant,b.run_id)');
END $$;
ALTER FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 RENAME TO editor_effect_seal_worker_trace_ready;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_worker_trace_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM photo_worker;
CREATE FUNCTION photo.editor_effect_seal_worker_ready(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 RETURN photo.editor_effect_seal_worker_trace_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_json,p_digest)
 AND photo.editor_effect_source_parent_current(p_tenant,(p_json::jsonb->>'build_run_id')::uuid);
EXCEPTION WHEN invalid_text_representation OR check_violation OR insufficient_privilege THEN RETURN false;
END $$;
ALTER FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.editor_effect_seal_worker_ready(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
