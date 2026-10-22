-- +goose Up
-- Private final composition authority. No public recipe or accepted-master
-- predicate is weakened: the original asset version stays unchanged.
ALTER TABLE photo.editor_effect_builds
 ADD COLUMN final_recipe_jcs text,
 ADD COLUMN final_recipe_sha text CHECK(final_recipe_sha ~ '^[0-9a-f]{64}$'),
 ADD COLUMN final_recipe_artifact_id uuid,
 ADD CONSTRAINT editor_final_recipe_bytes CHECK((final_recipe_jcs IS NULL)=(final_recipe_sha IS NULL)
  AND (final_recipe_jcs IS NULL OR octet_length(final_recipe_jcs) BETWEEN 2 AND 1048576
   AND encode(sha256(convert_to(final_recipe_jcs,'UTF8')),'hex')=final_recipe_sha));
-- +goose StatementBegin
CREATE FUNCTION photo.editor_effect_final_origin_ready(p_tenant uuid,p_build uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.editor_effect_builds b
 JOIN photo.runs original ON original.tenant_id=b.tenant_id AND original.id=b.run_id
 JOIN photo.runs sealed ON sealed.tenant_id=b.tenant_id AND sealed.id=b.seal_run_id
 JOIN photo.tasks task ON task.tenant_id=b.tenant_id AND task.id=b.seal_task_id AND task.run_id=sealed.id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.task_id=task.id AND result.id=task.canonical_result_id
 WHERE b.tenant_id=p_tenant AND b.run_id=p_build AND b.state IN('sealed','composing','complete')
 AND original.status='completed' AND NOT original.cancel_requested
 AND sealed.status='completed' AND NOT sealed.cancel_requested AND task.state='succeeded' AND NOT task.cancel_requested
 AND task.attempt=result.attempt AND task.fencing_token=result.fencing_token
 AND b.seal_adoption_digest IS NOT NULL AND b.seal_adoption_sql_digest IS NOT NULL
 AND photo.v2_editor_authorized(p_tenant,b.project_id,b.actor_id,b.session_id)
 AND photo.editor_effect_origin_scope(p_tenant,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id)
 AND photo.editor_effect_source_parent_current(p_tenant,p_build)
 AND EXISTS(SELECT FROM photo.tasks producer JOIN photo.task_results canonical
  ON canonical.tenant_id=producer.tenant_id AND canonical.id=producer.canonical_result_id AND canonical.task_id=producer.id
  WHERE producer.tenant_id=b.tenant_id AND producer.run_id=b.run_id AND producer.id=b.producer_task_id
  AND producer.state='succeeded' AND NOT producer.cancel_requested AND canonical.id=b.producer_result_id
  AND producer.attempt=b.producer_attempt AND producer.fencing_token=b.producer_fence
  AND canonical.attempt=b.producer_attempt AND canonical.fencing_token=b.producer_fence
  AND canonical.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha)))
 AND EXISTS(SELECT FROM photo.tasks quality JOIN photo.task_results canonical
  ON canonical.tenant_id=quality.tenant_id AND canonical.id=quality.canonical_result_id AND canonical.task_id=quality.id
  WHERE quality.tenant_id=b.tenant_id AND quality.run_id=b.run_id AND quality.id=b.quality_task_id
  AND quality.operation='quality.check' AND quality.state='succeeded' AND NOT quality.cancel_requested
  AND canonical.id=b.quality_result_id AND canonical.attempt=quality.attempt AND canonical.fencing_token=quality.fencing_token
  AND canonical.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',b.quality_artifact_id,'sha256',b.quality_sha)))
 AND EXISTS(SELECT FROM photo.artifacts receipt WHERE receipt.tenant_id=b.tenant_id
  AND receipt.id=b.seal_receipt_artifact_id AND receipt.sha256=b.seal_receipt_sha
  AND receipt.created_by_task_id=task.id AND receipt.produced_attempt=result.attempt AND receipt.produced_fence=result.fencing_token
  AND receipt.output_role='executed_base_receipt' AND receipt.state='ready' AND receipt.verified_at IS NOT NULL
  AND receipt.object_version IS NOT NULL AND receipt.object_version NOT IN('','null')
  AND (receipt.expires_at IS NULL OR receipt.expires_at>clock_timestamp())
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',receipt.id,'sha256',receipt.sha256)))
 AND EXISTS(SELECT FROM photo.artifacts root WHERE root.tenant_id=b.tenant_id
  AND root.id=b.result_artifact_id AND root.sha256=b.result_sha
  AND root.created_by_task_id=task.id AND root.produced_attempt=result.attempt AND root.produced_fence=result.fencing_token
  AND root.output_role='image_result' AND root.state='ready' AND root.verified_at IS NOT NULL
  AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null')
  AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256)))
 AND EXISTS(SELECT FROM photo.artifacts source WHERE source.tenant_id=b.tenant_id
  AND source.id=b.processed_source_artifact_id AND source.sha256=b.processed_source_sha
  AND source.state='ready' AND source.verified_at IS NOT NULL AND source.object_version IS NOT NULL
  AND source.object_version NOT IN('','null') AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp()))
 );
$$;
ALTER FUNCTION photo.editor_effect_final_origin_ready(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_final_origin_ready(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
ALTER FUNCTION photo.editor_effect_render_receipt(uuid,uuid,uuid) RENAME TO editor_effect_render_receipt_canonical;
DO $$ DECLARE definition text;needle text;replacement text;BEGIN
 definition:=pg_get_functiondef('photo.editor_effect_render_receipt_canonical(uuid,uuid,uuid)'::regprocedure);
 needle:='t.inputs @> jsonb_build_array(jsonb_build_object(''artifact_id'',a.id,''sha256'',a.sha256))';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_RECEIPT_TYPED_INPUT_ANCHOR';END IF;
 replacement:='EXISTS(SELECT FROM photo.v2_task_inputs typed WHERE typed.tenant_id=t.tenant_id AND typed.task_id=t.id AND typed.project_id=b.project_id AND typed.artifact_id=a.id AND typed.sha256=a.sha256 AND typed.contract=''EditorExecutedBaseReceipt'')';
 definition:=replace(definition,needle,replacement);
 needle:='t.inputs @> jsonb_build_array(jsonb_build_object(''artifact_id'',root.id,''sha256'',root.sha256))';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_RESULT_TYPED_INPUT_ANCHOR';END IF;
 replacement:='EXISTS(SELECT FROM photo.v2_task_inputs typed WHERE typed.tenant_id=t.tenant_id AND typed.task_id=t.id AND typed.project_id=b.project_id AND typed.artifact_id=root.id AND typed.sha256=root.sha256 AND typed.contract=''ImageResult'')';
 EXECUTE replace(definition,needle,replacement);
END $$;
CREATE FUNCTION photo.editor_effect_render_receipt(p_tenant uuid,p_run uuid,p_task uuid)
RETURNS TABLE(artifact_id uuid,sha256 text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT receipt.* FROM photo.editor_effect_render_receipt_canonical(p_tenant,p_run,p_task) receipt
 WHERE EXISTS(SELECT FROM photo.editor_effect_builds b WHERE b.tenant_id=p_tenant AND b.final_run_id=p_run
  AND photo.editor_effect_final_origin_ready(p_tenant,b.run_id));
$$;
ALTER FUNCTION photo.editor_effect_render_receipt(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_render_receipt(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
CREATE FUNCTION photo.editor_effect_final_artifact(p_tenant uuid,p_build uuid,p_artifact uuid,p_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.editor_effect_builds b JOIN photo.runs origin ON origin.tenant_id=b.tenant_id AND origin.id=b.run_id
 JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=p_artifact AND a.sha256=p_sha
 WHERE b.tenant_id=p_tenant AND b.run_id=p_build AND photo.editor_effect_final_origin_ready(p_tenant,p_build)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.bytes>0
 AND (
  origin.config_snapshot#>'{editor_effect_source,inputs}' @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
  OR a.id=b.executed_style_artifact_id AND a.sha256=b.executed_style_sha
  OR a.id=b.final_recipe_artifact_id AND a.sha256=b.final_recipe_sha AND EXISTS(SELECT FROM photo.backend_documents d
   WHERE d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND d.artifact_id=a.id AND d.sha256=a.sha256 AND d.state='ready'
   AND d.purpose='task_params' AND d.editor_effects_params=b.final_recipe_jcs
   AND d.object_version=a.object_version AND d.bucket=a.bucket AND d.bytes=a.bytes)
  OR EXISTS(SELECT FROM photo.tasks t JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
   WHERE t.tenant_id=b.tenant_id AND t.run_id IN(b.run_id,b.seal_run_id) AND t.state='succeeded' AND NOT t.cancel_requested
   AND t.attempt=z.attempt AND t.fencing_token=z.fencing_token
   AND (t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))
    OR a.created_by_task_id=t.id AND a.produced_attempt=z.attempt AND a.produced_fence=z.fencing_token
     AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256))))
 );
$$;
ALTER FUNCTION photo.editor_effect_final_artifact(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_final_artifact(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.editor_effect_final_artifact(uuid,uuid,uuid,text) TO photo_scheduler;
CREATE FUNCTION photo.prepare_editor_effect_final_recipe(p_tenant uuid,p_build uuid,p_recipe text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;body jsonb;recipe jsonb;binding jsonb;style jsonb;zero_delta jsonb;
BEGIN
 IF p_recipe IS NULL OR octet_length(p_recipe) NOT BETWEEN 2 AND 1048576 THEN RETURN false;END IF;
 PERFORM run_id FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF NOT FOUND OR b.state NOT IN('sealed','composing') OR NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN false;END IF;
 body:=p_recipe::jsonb;recipe:=body->'recipe';
 binding:=jsonb_build_object('asset_version_id',b.asset_version_id,'source',jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha),'canvas_hash',b.processed_source_canvas_hash);
 style:=jsonb_build_object('artifact_id',b.executed_style_artifact_id,'sha256',b.executed_style_sha);
 zero_delta:='{"ev":0,"log_r":0,"log_g":0,"log_b":0,"saturation_delta":0,"retouch_delta":0}'::jsonb;
 IF jsonb_typeof(body)<>'object' OR body-'schema_version'-'recipe'-'recipe_sha256'-'scalars'-'learning'<>'{}'::jsonb
 OR body->>'schema_version' IS DISTINCT FROM '1' OR body->'learning' IS DISTINCT FROM 'null'::jsonb
 OR recipe-'schema_version'-'source'-'base_style'-'session_corrections'-'learning_version_id'-'overrides'-'layer_stack'-'renderer_version'-'parameter_delta'-'executed_base_receipt'<>'{}'::jsonb
 OR recipe->>'schema_version' IS DISTINCT FROM '3' OR recipe->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR recipe->'source' IS DISTINCT FROM binding OR recipe->'base_style' IS DISTINCT FROM style
 OR recipe->'executed_base_receipt' IS DISTINCT FROM jsonb_build_object('artifact_id',b.seal_receipt_artifact_id,'sha256',b.seal_receipt_sha)
 OR recipe->'session_corrections' IS DISTINCT FROM 'null'::jsonb OR recipe->'learning_version_id' IS DISTINCT FROM 'null'::jsonb
 OR recipe->'layer_stack' IS DISTINCT FROM 'null'::jsonb OR recipe->'overrides' IS DISTINCT FROM '[]'::jsonb
 OR recipe->'parameter_delta' IS DISTINCT FROM zero_delta
 OR body->'scalars'->'source' IS DISTINCT FROM binding OR body->'scalars'->'base_style' IS DISTINCT FROM style
 OR body->'scalars'->'effective_delta' IS DISTINCT FROM zero_delta
 OR jsonb_typeof(body->'scalars') IS DISTINCT FROM 'object'
 OR (body->'scalars')-'schema_version'-'source'-'base_style'-'effective_delta'-'renderer_version'-'icc_lock_sha'-'model_lock_sha'<>'{}'::jsonb
 OR body->'scalars'->>'schema_version' IS DISTINCT FROM '1' OR body->'scalars'->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR coalesce(body->'scalars'->>'icc_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(body->'scalars'->>'model_lock_sha','') !~ '^[0-9a-f]{64}$'
 OR coalesce(body->>'recipe_sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 IF b.final_recipe_jcs IS NOT NULL THEN RETURN b.final_recipe_jcs=p_recipe;END IF;
 UPDATE photo.editor_effect_builds SET final_recipe_jcs=p_recipe,final_recipe_sha=encode(sha256(convert_to(p_recipe,'UTF8')),'hex'),updated_at=clock_timestamp()
 WHERE tenant_id=p_tenant AND run_id=p_build;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
CREATE FUNCTION photo.begin_editor_effect_final_render(p_tenant uuid,p_build uuid,p_run uuid,p_workflow uuid,p_config text,p_policy text,p_bytes bigint,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;origin photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;config jsonb;node jsonb;current_pin record;h bytea;expected uuid;old photo.runs%ROWTYPE;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM run_id FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 SELECT * INTO origin FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state NOT IN('sealed','composing') OR NOT photo.editor_effect_final_origin_ready(p_tenant,p_build)
 OR p_config IS NULL OR octet_length(p_config) NOT BETWEEN 2 AND 1048576 OR p_bytes NOT BETWEEN 1 AND 9007199254740991 THEN RETURN false;END IF;
 h:=public.digest(uuid_send(p_build)||convert_to('editor.effects_final/run','UTF8'),'sha1');h:=set_byte(h,6,(get_byte(h,6)&15)|80);h:=set_byte(h,8,(get_byte(h,8)&63)|128);
 expected:=encode(substring(h FROM 1 FOR 16),'hex')::uuid;IF p_run IS DISTINCT FROM expected THEN RETURN false;END IF;
 config:=p_config::jsonb;node:=config#>'{v26_render,nodes,0}';
 IF jsonb_array_length(config#>'{v26_render,nodes}')<>1 OR node->>'task_id' IS DISTINCT FROM photo.editor_effect_final_task_id(p_build)::text
 OR config->>'project_id' IS DISTINCT FROM b.project_id::text OR config->>'input_snapshot_id' IS DISTINCT FROM origin.input_snapshot_id::text
 OR config#>>'{v26_render,auth_session_id}' IS DISTINCT FROM b.session_id::text
 OR node->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',b.final_recipe_artifact_id,'sha256',b.final_recipe_sha)
 OR node->>'params_jcs' IS DISTINCT FROM b.final_recipe_jcs OR node->>'source_jcs' IS NULL
 OR encode(sha256(convert_to(node->>'source_jcs','UTF8')),'hex') IS DISTINCT FROM b.processed_source_sha
 OR coalesce(node->>'layer_jcs','')<>'' OR coalesce(node->>'protection_jcs','')<>''
 OR NOT EXISTS(SELECT FROM photo.artifacts a JOIN photo.backend_documents d ON d.tenant_id=a.tenant_id AND d.artifact_id=a.id
  WHERE a.tenant_id=p_tenant AND a.id=b.final_recipe_artifact_id AND a.sha256=b.final_recipe_sha
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND d.run_id=p_build AND d.owner_id=photo.editor_effect_final_task_id(p_build)
  AND d.state='ready' AND d.editor_effects_params=b.final_recipe_jcs AND d.object_version=a.object_version)
 OR jsonb_typeof(p_pin) IS DISTINCT FROM 'object' OR p_pin-'model_lock_sha'-'runtime_lock_sha'-'backend'-'device_profile_sha'-'dataset_sha'-'calibration_sha'<>'{}'::jsonb
 OR coalesce(p_pin->>'calibration_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 SELECT * INTO current_pin FROM photo.resolve_v26_render_qualification(p_tenant,b.project_id);
 IF NOT FOUND OR p_pin->>'model_lock_sha' IS DISTINCT FROM current_pin.model_lock_sha
 OR p_pin->>'runtime_lock_sha' IS DISTINCT FROM current_pin.runtime_lock_sha OR p_pin->>'backend' IS DISTINCT FROM current_pin.backend
 OR p_pin->>'device_profile_sha' IS DISTINCT FROM current_pin.device_profile_sha OR p_pin->>'dataset_sha' IS DISTINCT FROM current_pin.dataset_sha
 OR node#>>'{profile,model_lock_sha}' IS DISTINCT FROM current_pin.model_lock_sha
 OR node#>>'{profile,render_runtime_sha}' IS DISTINCT FROM current_pin.runtime_lock_sha OR node#>>'{profile,backend}' IS DISTINCT FROM current_pin.backend THEN RETURN false;END IF;
 SELECT * INTO old FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF old.id IS NULL THEN
  SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_build AND state='reserved' AND expires_at>clock_timestamp() FOR UPDATE;
  IF q.id IS NULL THEN RETURN false;END IF;
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=p_tenant AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'EDITOR_FINAL_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET state='consumed',bytes=0 WHERE tenant_id=p_tenant AND id=q.id;
  UPDATE photo.editor_effect_builds SET state='composing',final_run_id=p_run,updated_at=clock_timestamp() WHERE tenant_id=p_tenant AND run_id=p_build;
 ELSE
  IF b.final_run_id IS DISTINCT FROM p_run OR b.state<>'composing' OR old.v26_qualification_pin IS DISTINCT FROM p_pin THEN RETURN false;END IF;
 END IF;
 IF NOT photo.begin_v26_render_draft(p_tenant,b.project_id,origin.input_snapshot_id,b.actor_id,b.session_id,p_run,p_workflow,p_config,p_policy,p_bytes) THEN
  RAISE EXCEPTION 'EDITOR_FINAL_PLAN_REJECTED' USING ERRCODE='P2674';END IF;
 IF old.id IS NULL THEN UPDATE photo.runs SET v26_qualification_pin=p_pin WHERE tenant_id=p_tenant AND id=p_run;END IF;
 RETURN true;
EXCEPTION WHEN SQLSTATE 'P2674' OR invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.begin_editor_effect_final_render(uuid,uuid,uuid,uuid,text,text,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.begin_editor_effect_final_render(uuid,uuid,uuid,uuid,text,text,bigint,jsonb) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.begin_editor_effect_final_render(uuid,uuid,uuid,uuid,text,text,bigint,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.prepare_editor_effect_final_recipe(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.prepare_editor_effect_final_recipe(uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.prepare_editor_effect_final_recipe(uuid,uuid,text) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_final_task_id(p_build uuid) RETURNS uuid
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE namespace uuid:=p_build;label text;digest bytea;
BEGIN
 FOREACH label IN ARRAY ARRAY['editor.effects_final/run','workflow','render.compose:0'] LOOP
  digest:=public.digest(uuid_send(namespace)||convert_to(label,'UTF8'),'sha1');
  digest:=set_byte(digest,6,(get_byte(digest,6)&15)|80);digest:=set_byte(digest,8,(get_byte(digest,8)&63)|128);
  namespace:=encode(substring(digest FROM 1 FOR 16),'hex')::uuid;
 END LOOP;
 RETURN namespace;
END $$;
ALTER FUNCTION photo.editor_effect_final_task_id(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_final_task_id(uuid) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
CREATE FUNCTION photo.reserve_editor_effect_final_recipe(p_tenant uuid,p_build uuid,p_owner uuid,p_bucket text)
RETURNS TABLE(artifact_id uuid,token uuid,ready boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;d photo.backend_documents%ROWTYPE;usage photo.tenant_usage%ROWTYPE;capacity numeric;size bigint;
BEGIN
 IF p_owner IS DISTINCT FROM photo.editor_effect_final_task_id(p_build) OR length(coalesce(p_bucket,'')) NOT BETWEEN 3 AND 63 THEN RETURN;END IF;
 SELECT (quotas->>'storage_bytes')::numeric INTO capacity FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO usage FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state NOT IN('sealed','composing') OR b.final_recipe_jcs IS NULL OR NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN;END IF;
 size:=octet_length(b.final_recipe_jcs);
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_build AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF FOUND THEN
  IF d.artifact_id=b.final_recipe_artifact_id AND d.sha256=b.final_recipe_sha AND d.bytes=size AND d.bucket=p_bucket
   AND d.editor_effects_params=b.final_recipe_jcs AND d.state IN('pending','ready')
   AND (d.state='pending' OR EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=d.artifact_id AND a.sha256=d.sha256
    AND a.bytes=d.bytes AND a.bucket=d.bucket AND a.object_version=d.object_version AND a.object_version NOT IN('','null')
    AND a.object_key='tenants/'||p_tenant||'/runs/'||p_build||'/documents/'||d.artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL
    AND a.media_type='application/json' AND a.output_role='task_params' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))) THEN RETURN QUERY SELECT d.artifact_id,d.token,d.state='ready';END IF;RETURN;
 END IF;
 IF b.final_recipe_artifact_id IS NOT NULL OR capacity IS NULL OR usage.tenant_id IS NULL
 OR usage.storage_bytes::numeric+usage.reserved_bytes+size>capacity OR usage.storage_bytes::numeric+usage.reserved_bytes+size>9007199254740991 THEN RETURN;END IF;
 INSERT INTO photo.backend_documents(tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,bucket,editor_effects_params)
 VALUES(p_tenant,p_build,p_owner,'task_params',gen_random_uuid(),gen_random_uuid(),b.final_recipe_sha,size,p_bucket,b.final_recipe_jcs) RETURNING * INTO d;
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+size WHERE tenant_id=p_tenant;
 UPDATE photo.editor_effect_builds SET final_recipe_artifact_id=d.artifact_id WHERE tenant_id=p_tenant AND run_id=p_build;
 RETURN QUERY SELECT d.artifact_id,d.token,false;
END $$;
ALTER FUNCTION photo.reserve_editor_effect_final_recipe(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.reserve_editor_effect_final_recipe(uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.reserve_editor_effect_final_recipe(uuid,uuid,uuid,text) TO photo_scheduler;
CREATE FUNCTION photo.complete_editor_effect_final_recipe(p_tenant uuid,p_build uuid,p_owner uuid,p_artifact uuid,p_token uuid,p_sha text,p_bytes bigint,p_bucket text,p_version text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;d photo.backend_documents%ROWTYPE;target text;
BEGIN
 IF p_owner IS DISTINCT FROM photo.editor_effect_final_task_id(p_build) OR p_version IS NULL OR p_version IN('','null') OR length(p_version)>1024 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.run_id IS NULL OR b.state NOT IN('sealed','composing') OR NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN false;END IF;
 SELECT * INTO d FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_build AND owner_id=p_owner AND purpose='task_params' FOR UPDATE;
 IF d.id IS NULL OR d.artifact_id IS DISTINCT FROM p_artifact OR b.final_recipe_artifact_id IS DISTINCT FROM p_artifact
 OR d.token IS DISTINCT FROM p_token OR d.sha256 IS DISTINCT FROM p_sha OR b.final_recipe_sha IS DISTINCT FROM p_sha
 OR d.bytes IS DISTINCT FROM p_bytes OR d.bucket IS DISTINCT FROM p_bucket OR d.editor_effects_params IS DISTINCT FROM b.final_recipe_jcs THEN RETURN false;END IF;
 target:='tenants/'||p_tenant||'/runs/'||p_build||'/documents/'||p_artifact;
 IF d.state='ready' THEN RETURN d.object_version=p_version AND EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_artifact
  AND a.sha256=p_sha AND a.bytes=p_bytes AND a.state='ready' AND a.object_version=p_version AND a.bucket=p_bucket AND a.object_key=target AND a.verified_at IS NOT NULL
  AND a.media_type='application/json' AND a.output_role='task_params' AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));END IF;
 IF d.state<>'pending' THEN RETURN false;END IF;
 INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at,output_role)
 VALUES(p_artifact,p_tenant,p_bucket,target,p_version,p_sha,p_bytes,'application/json','ready',clock_timestamp(),'task_params');
 UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-p_bytes,storage_bytes=storage_bytes+p_bytes WHERE tenant_id=p_tenant AND reserved_bytes>=p_bytes;
 IF NOT FOUND THEN RAISE EXCEPTION 'EDITOR_FINAL_DOCUMENT_ACCOUNTING' USING ERRCODE='23514';END IF;
 UPDATE photo.backend_documents SET state='ready',object_version=p_version,completed_at=clock_timestamp() WHERE tenant_id=p_tenant AND id=d.id;
 RETURN true;
END $$;
ALTER FUNCTION photo.complete_editor_effect_final_recipe(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.complete_editor_effect_final_recipe(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.complete_editor_effect_final_recipe(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_final_context(p_after uuid)
RETURNS TABLE(tenant_id uuid,build_run_id uuid,origin jsonb)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT b.tenant_id,b.run_id,to_jsonb(b)||jsonb_build_object('input_snapshot_id',r.input_snapshot_id)
 FROM photo.editor_effect_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id
 WHERE (b.state='sealed' OR b.state='composing' AND EXISTS(SELECT FROM photo.runs final WHERE final.tenant_id=b.tenant_id AND final.id=b.final_run_id AND final.status='draft' AND NOT final.graph_sealed AND NOT final.cancel_requested))
 AND (p_after IS NULL OR b.run_id>p_after)
 AND photo.editor_effect_final_origin_ready(b.tenant_id,b.run_id)
 ORDER BY b.run_id LIMIT 1;
$$;
ALTER FUNCTION photo.editor_effect_final_context(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_final_context(uuid) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.editor_effect_final_context(uuid) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_final_qualification(p_tenant uuid,p_build uuid)
RETURNS TABLE(model_lock_sha text,runtime_lock_sha text,backend text,device_profile_sha text,dataset_sha text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE project uuid;
BEGIN
 IF NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN;END IF;
 SELECT project_id INTO project FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 RETURN QUERY SELECT q.* FROM photo.resolve_v26_render_qualification(p_tenant,project) q;
END $$;
ALTER FUNCTION photo.editor_effect_final_qualification(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_final_qualification(uuid,uuid) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.editor_effect_final_qualification(uuid,uuid) TO photo_scheduler;
ALTER FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) RENAME TO v26_render_recipe_scope_before_editor;
CREATE FUNCTION photo.v26_render_recipe_scope(p_tenant uuid,p_project uuid,p_version uuid,p_artifact uuid,p_sha text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v26_render_recipe_scope_before_editor(p_tenant,p_project,p_version,p_artifact,p_sha,p_actor,p_session)
 OR EXISTS(SELECT FROM photo.editor_effect_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.final_run_id
  JOIN photo.backend_documents d ON d.tenant_id=b.tenant_id AND d.run_id=b.run_id AND d.artifact_id=b.final_recipe_artifact_id
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id AND a.sha256=d.sha256
  WHERE b.tenant_id=p_tenant AND b.project_id=p_project AND b.asset_version_id=p_version AND b.actor_id=p_actor AND b.session_id=p_session
  AND b.final_recipe_artifact_id=p_artifact AND b.final_recipe_sha=p_sha AND b.state IN('composing','complete')
  AND photo.editor_effect_final_origin_ready(p_tenant,b.run_id) AND r.kind='v2' AND NOT r.cancel_requested
  AND d.state='ready' AND d.editor_effects_params=b.final_recipe_jcs AND d.object_version=a.object_version
  AND d.sha256=b.final_recipe_sha AND d.bytes=octet_length(b.final_recipe_jcs)
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
$$;
ALTER FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_render_recipe_scope(uuid,uuid,uuid,uuid,text,uuid,uuid) TO photo_api;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) RENAME TO v2_draft_artifact_scope_before_editor;
CREATE FUNCTION photo.v2_draft_artifact_scope(p_tenant uuid,p_project uuid,p_version uuid,p_artifact uuid,p_sha text,p_contract text,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v2_draft_artifact_scope_before_editor(p_tenant,p_project,p_version,p_artifact,p_sha,p_contract,p_actor,p_session)
 OR p_contract='StyleV2' AND EXISTS(SELECT FROM photo.editor_effect_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.final_run_id
  JOIN photo.artifacts a ON a.tenant_id=b.tenant_id AND a.id=b.executed_style_artifact_id AND a.sha256=b.executed_style_sha
  WHERE b.tenant_id=p_tenant AND b.project_id=p_project AND b.asset_version_id=p_version AND b.actor_id=p_actor AND b.session_id=p_session
  AND a.id=p_artifact AND a.sha256=p_sha AND b.state IN('composing','complete') AND photo.editor_effect_final_origin_ready(p_tenant,b.run_id)
  AND r.kind='v2' AND NOT r.cancel_requested AND a.state='ready' AND a.verified_at IS NOT NULL
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
$$;
ALTER FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v2_draft_artifact_scope(uuid,uuid,uuid,uuid,text,text,uuid,uuid) TO photo_api;
CREATE FUNCTION photo.activate_editor_effect_final_render(p_tenant uuid,p_build uuid,p_run uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE b photo.editor_effect_builds%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;ref jsonb;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 PERFORM run_id FROM photo.apply_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_build FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=p_tenant AND run_id=p_build FOR UPDATE;
 IF b.final_run_id IS DISTINCT FROM p_run OR b.state<>'composing' OR NOT photo.editor_effect_final_origin_ready(p_tenant,p_build) THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=photo.editor_effect_final_task_id(p_build) FOR UPDATE;
 IF r.id IS NULL OR t.id IS NULL OR r.cancel_requested OR t.cancel_requested OR r.kind<>'v2'
 OR r.status NOT IN('draft','queued','running') OR r.v26_qualification_pin IS NULL
 OR t.params_artifact_id IS DISTINCT FROM b.final_recipe_artifact_id
 OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN false;END IF;
 FOR ref IN SELECT value FROM jsonb_build_array(
  jsonb_build_object('artifact_id',b.seal_receipt_artifact_id,'sha256',b.seal_receipt_sha,'contract','EditorExecutedBaseReceipt'),
  jsonb_build_object('artifact_id',b.result_artifact_id,'sha256',b.result_sha,'contract','ImageResult'),
  jsonb_build_object('artifact_id',b.processed_source_artifact_id,'sha256',b.processed_source_sha,'contract','WorkingImage'),
  jsonb_build_object('artifact_id',b.executed_style_artifact_id,'sha256',b.executed_style_sha,'contract','StyleV2')) LOOP
  IF NOT EXISTS(SELECT FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND i.artifact_id::text=ref->>'artifact_id'
    AND i.sha256=ref->>'sha256' AND i.contract=ref->>'contract') THEN RETURN false;END IF;
 END LOOP;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_render_qualification_ready(p_tenant,p_run) THEN RETURN false;END IF;
 IF r.graph_sealed THEN RETURN r.status IN('queued','running');END IF;
 IF NOT photo.seal_v2_graph(p_tenant,p_run) THEN RAISE EXCEPTION 'EDITOR_FINAL_ACTIVATION_REJECTED' USING ERRCODE='P2674';END IF;
 RETURN true;
EXCEPTION WHEN SQLSTATE 'P2674' OR invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.activate_editor_effect_final_render(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_editor_effect_final_render(uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.activate_editor_effect_final_render(uuid,uuid,uuid) TO photo_scheduler;
CREATE FUNCTION photo.editor_effect_final_snapshot_scope(p_tenant uuid,p_run uuid,p_snapshot uuid,p_version uuid,p_source uuid,p_sha text)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT FROM photo.editor_effect_builds b JOIN photo.runs origin ON origin.tenant_id=b.tenant_id AND origin.id=b.run_id
 WHERE b.tenant_id=p_tenant AND b.final_run_id=p_run AND b.asset_version_id=p_version AND origin.input_snapshot_id=p_snapshot
 AND b.processed_source_artifact_id=p_source AND b.processed_source_sha=p_sha AND b.state='composing'
 AND photo.editor_effect_final_origin_ready(p_tenant,b.run_id));
$$;
ALTER FUNCTION photo.editor_effect_final_snapshot_scope(uuid,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.editor_effect_final_snapshot_scope(uuid,uuid,uuid,uuid,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler;
DO $$ DECLARE definition text;needle text;replacement text;BEGIN
 definition:=pg_get_functiondef('photo.stage_v26_render_recipe_task(uuid,uuid,uuid,integer,uuid,text,text,jsonb)'::regprocedure);
 needle:='OR NOT EXISTS(SELECT 1 FROM photo.snapshot_items item WHERE item.tenant_id=p_tenant AND item.snapshot_id=r.input_snapshot_id
   AND item.asset_version_id=version_id AND item.artifact_id=(binding->''source''->>''artifact_id'')::uuid
   AND item.sha256=binding->''source''->>''sha256'')';
 IF (length(definition)-length(replace(definition,needle,'')))/length(needle)<>1 THEN RAISE EXCEPTION 'EDITOR_FINAL_STAGE_SNAPSHOT_ANCHOR';END IF;
 replacement:='OR NOT ('||substring(needle FROM 8)||' OR photo.editor_effect_final_snapshot_scope(p_tenant,p_run,r.input_snapshot_id,version_id,(binding->''source''->>''artifact_id'')::uuid,binding->''source''->>''sha256''))';
 EXECUTE replace(definition,needle,replacement);
END $$;
-- +goose StatementEnd
-- Final lifecycle is separate from original Apply completion. A canonical
-- render result must have passed the independent adopter and all reviews.
-- +goose StatementBegin
CREATE FUNCTION photo.settle_editor_effect_final_render() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;b photo.editor_effect_builds%ROWTYPE;r photo.runs%ROWTYPE;
BEGIN
 SELECT o.tenant_id,o.run_id INTO candidate FROM photo.editor_effect_builds o
 JOIN photo.runs final ON final.tenant_id=o.tenant_id AND final.id=o.final_run_id
 WHERE o.state='composing' AND final.status IN('completed','completed_with_errors','failed','cancelled')
 ORDER BY o.run_id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.apply_builds WHERE tenant_id=candidate.tenant_id AND run_id=candidate.run_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.run_id FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=candidate.tenant_id AND run_id=candidate.run_id FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=b.tenant_id AND id=b.final_run_id FOR UPDATE;
 IF b.state<>'composing' OR r.status NOT IN('completed','completed_with_errors','failed','cancelled') THEN RETURN false;END IF;
 -- The withdrawal path owns terminal failure and quota release. Never hide a
 -- failed origin from that cleanup by marking it failed here.
 IF r.status IN('completed_with_errors','failed','cancelled') THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tasks WHERE tenant_id=b.tenant_id AND run_id=r.id ORDER BY id FOR SHARE;
 PERFORM 1 FROM photo.reviews WHERE tenant_id=b.tenant_id AND run_id=r.id ORDER BY id FOR SHARE;
 IF NOT photo.editor_effect_final_origin_ready(b.tenant_id,b.run_id) OR NOT r.graph_sealed OR r.cancel_requested
  OR (SELECT count(*) FROM photo.tasks WHERE tenant_id=b.tenant_id AND run_id=r.id)<>1
  OR EXISTS(SELECT FROM photo.reviews WHERE tenant_id=b.tenant_id AND run_id=r.id AND state='pending')
  OR NOT EXISTS(SELECT FROM photo.tasks task JOIN photo.task_results result
   ON result.tenant_id=task.tenant_id AND result.task_id=task.id AND result.id=task.canonical_result_id
   WHERE task.tenant_id=b.tenant_id AND task.run_id=r.id AND task.id=photo.editor_effect_final_task_id(b.run_id)
    AND task.operation='render.compose' AND task.operation_version='1' AND task.state='succeeded' AND NOT task.cancel_requested
    AND task.attempt=result.attempt AND task.fencing_token=result.fencing_token
    AND photo.v26_render_task_result_ready(task.tenant_id,task.id)
    AND EXISTS(SELECT FROM photo.inbox inbox WHERE inbox.tenant_id=task.tenant_id AND inbox.consumer='v2-adopt'
     AND inbox.event_id=photo.v2_adoption_event_id(task.id,result.id))
    AND (NOT result.review_required OR photo.v26_render_review_completion_ready(task.tenant_id,task.id))) THEN RETURN false;END IF;
 UPDATE photo.editor_effect_builds SET state='complete' WHERE tenant_id=b.tenant_id AND run_id=b.run_id;RETURN true;
END $$;
ALTER FUNCTION photo.settle_editor_effect_final_render() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.settle_editor_effect_final_render() FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.settle_editor_effect_final_render() TO photo_scheduler;
-- +goose StatementEnd
-- Authority withdrawal never changes a completed source asset. Active stage
-- runs enter ordinary cancellation; reservations settle only after no live
-- task can publish another result.
-- +goose StatementBegin
CREATE FUNCTION photo.withdraw_editor_effect_build() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE candidate record;b photo.editor_effect_builds%ROWTYPE;r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;changed boolean:=false;
BEGIN
 SELECT o.tenant_id,o.run_id INTO candidate FROM photo.editor_effect_builds o
 WHERE o.state NOT IN('complete','failed') AND (
  NOT photo.v2_editor_authorized(o.tenant_id,o.project_id,o.actor_id,o.session_id)
  OR NOT photo.editor_effect_origin_scope(o.tenant_id,o.project_id,o.asset_version_id,o.source_artifact_id,o.source_sha,o.frozen_recipe_artifact_id,o.frozen_recipe_sha,o.frozen_document_id,o.draft_document_id,o.draft_revision,o.actor_id,o.session_id)
  OR NOT photo.editor_effect_source_parent_current(o.tenant_id,o.run_id)
  OR EXISTS(SELECT FROM photo.runs stage WHERE stage.tenant_id=o.tenant_id AND stage.id IN(o.run_id,o.seal_run_id,o.final_run_id)
   AND (stage.cancel_requested OR stage.status IN('failed','cancelled','completed_with_errors'))))
 ORDER BY o.run_id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.tenants WHERE id=candidate.tenant_id FOR UPDATE;
 PERFORM 1 FROM photo.tenant_usage WHERE tenant_id=candidate.tenant_id FOR UPDATE;
 PERFORM 1 FROM photo.apply_builds WHERE tenant_id=candidate.tenant_id AND run_id=candidate.run_id FOR UPDATE SKIP LOCKED;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.runs WHERE tenant_id=candidate.tenant_id AND id=candidate.run_id FOR UPDATE;
 SELECT * INTO b FROM photo.editor_effect_builds WHERE tenant_id=candidate.tenant_id AND run_id=candidate.run_id FOR UPDATE;
 IF b.state IN('complete','failed') THEN RETURN false;END IF;
 IF photo.v2_editor_authorized(b.tenant_id,b.project_id,b.actor_id,b.session_id)
  AND photo.editor_effect_origin_scope(b.tenant_id,b.project_id,b.asset_version_id,b.source_artifact_id,b.source_sha,b.frozen_recipe_artifact_id,b.frozen_recipe_sha,b.frozen_document_id,b.draft_document_id,b.draft_revision,b.actor_id,b.session_id)
  AND photo.editor_effect_source_parent_current(b.tenant_id,b.run_id)
  AND NOT EXISTS(SELECT FROM photo.runs stage WHERE stage.tenant_id=b.tenant_id AND stage.id IN(b.run_id,b.seal_run_id,b.final_run_id)
   AND (stage.cancel_requested OR stage.status IN('failed','cancelled','completed_with_errors'))) THEN RETURN false;END IF;
 FOR r IN SELECT * FROM photo.runs WHERE tenant_id=b.tenant_id AND id IN(b.run_id,b.seal_run_id,b.final_run_id) ORDER BY id FOR UPDATE LOOP
  IF r.status NOT IN('completed','completed_with_errors','failed','cancelled') AND NOT r.cancel_requested THEN
   IF r.revision>=9007199254740991 THEN RAISE EXCEPTION 'REVISION_EXHAUSTED' USING ERRCODE='23514';END IF;
   UPDATE photo.runs SET cancel_requested=true,status='cancelling',revision=revision+1 WHERE tenant_id=b.tenant_id AND id=r.id;changed:=true;
  END IF;
 END LOOP;
 IF EXISTS(SELECT FROM photo.runs stage WHERE stage.tenant_id=b.tenant_id AND stage.id IN(b.run_id,b.seal_run_id,b.final_run_id)
  AND stage.status NOT IN('completed','completed_with_errors','failed','cancelled'))
  OR EXISTS(SELECT FROM photo.tasks task WHERE task.tenant_id=b.tenant_id AND task.run_id IN(b.run_id,b.seal_run_id,b.final_run_id)
   AND task.state NOT IN('succeeded','failed','cancelled','skipped')) THEN RETURN changed;END IF;
 FOR q IN SELECT * FROM photo.quota_reservations WHERE tenant_id=b.tenant_id AND holder_type='run'
  AND holder_id IN(b.run_id,b.seal_run_id,b.final_run_id) AND state='reserved' ORDER BY id FOR UPDATE LOOP
  UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-q.bytes WHERE tenant_id=b.tenant_id AND reserved_bytes>=q.bytes;
  IF NOT FOUND THEN RAISE EXCEPTION 'EDITOR_WITHDRAW_QUOTA_INCONSISTENT' USING ERRCODE='23514';END IF;
  UPDATE photo.quota_reservations SET state='released',bytes=0 WHERE tenant_id=b.tenant_id AND id=q.id;
 END LOOP;
 UPDATE photo.editor_effect_builds SET state='failed',updated_at=clock_timestamp() WHERE tenant_id=b.tenant_id AND run_id=b.run_id;RETURN true;
END $$;
ALTER FUNCTION photo.withdraw_editor_effect_build() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.withdraw_editor_effect_build() FROM PUBLIC,photo_api,photo_worker;
GRANT EXECUTE ON FUNCTION photo.withdraw_editor_effect_build() TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
