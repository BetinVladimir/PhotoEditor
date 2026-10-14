-- +goose Up
-- Source locators are returned only after live policy/report/calibration locking.
-- Caller commits this short transaction before object IO and rechecks at admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_qualification_sources(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_pin jsonb,p_operation text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF photo.lock_v29_multi_qualification(p_tenant,p_project,p_actor,p_session,p_pin,p_operation) IS NOT TRUE THEN RETURN NULL;END IF;
 SELECT jsonb_agg(jsonb_build_object('contract',refs.contract,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes) ORDER BY refs.contract)
 INTO actual
 FROM (VALUES ('Report'::text,p_pin->'report'),('Calibration'::text,p_pin->'calibration')) refs(contract,ref)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=(refs.ref->>'artifact_id')::uuid AND a.sha256=refs.ref->>'sha256';
 IF jsonb_array_length(actual) IS DISTINCT FROM 2 THEN RETURN NULL;END IF;
 RETURN actual;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_qualification_sources(uuid,uuid,uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_qualification_sources(uuid,uuid,uuid,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_multi_qualification_sources(uuid,uuid,uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd

-- Completed-task original Params and current source/qualification snapshot.
-- No object IO; caller verifies original bytes and independently replays pixels.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_adoption_evidence(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;
 body jsonb;pin jsonb;locations jsonb;params_sha text;previous_tenant text;binding jsonb;operation text;frame_count integer;
BEGIN
 IF p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 body:=p_body::jsonb;params_sha:=encode(sha256(convert_to(p_body,'UTF8')),'hex');
 SELECT * INTO context FROM photo.v2_adoption_context(p_tenant,p_task);
 IF NOT FOUND OR context.attempt IS DISTINCT FROM p_attempt OR context.fence IS DISTINCT FROM p_fence
 OR context.result_id IS DISTINCT FROM p_result OR context.operation NOT IN ('optical.panorama','optical.focus_stack')
 OR context.operation_version IS DISTINCT FROM '1' THEN RETURN NULL;END IF;
 operation:=context.operation;
 IF jsonb_typeof(body) IS DISTINCT FROM 'object' OR body->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR NOT(body ?& ARRAY['schema_version','frames','reference_ordinal','max_pixels','calibration'])
 OR jsonb_typeof(body->'max_pixels') IS DISTINCT FROM 'number'
 OR coalesce(body->>'max_pixels','') !~ '^[0-9]+$'
 OR (body->>'max_pixels')::bigint NOT BETWEEN 1 AND 200000000 THEN RETURN NULL;END IF;
 IF operation='optical.panorama' THEN
  IF coalesce(body->>'projection','') NOT IN ('planar','cylindrical')
  OR (body-'schema_version'-'frames'-'reference_ordinal'-'max_pixels'-'calibration'-'projection')<>'{}'::jsonb THEN RETURN NULL;END IF;
 ELSE
  IF coalesce(body->>'motion_policy','') NOT IN ('review','reject')
  OR (body-'schema_version'-'frames'-'reference_ordinal'-'max_pixels'-'calibration'-'motion_policy')<>'{}'::jsonb THEN RETURN NULL;END IF;
 END IF;
 IF jsonb_typeof(body->'frames') IS DISTINCT FROM 'array' THEN RETURN NULL;END IF;
 frame_count:=jsonb_array_length(body->'frames');
 IF frame_count NOT BETWEEN 2 AND CASE operation WHEN 'optical.panorama' THEN 20 ELSE 30 END
 OR jsonb_typeof(body->'reference_ordinal') IS DISTINCT FROM 'number'
 OR (body->>'reference_ordinal') !~ '^[0-9]+$'
 OR (body->>'reference_ordinal')::integer NOT BETWEEN 0 AND frame_count-1
 OR (SELECT count(DISTINCT value->>'asset_version_id') FROM jsonb_array_elements(body->'frames'))<>frame_count THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 IF t.state IS DISTINCT FROM 'succeeded' OR t.cancel_requested IS DISTINCT FROM false
 OR t.attempt IS DISTINCT FROM p_attempt OR t.fencing_token IS DISTINCT FROM p_fence
 OR t.canonical_result_id IS DISTINCT FROM p_result OR t.resource_class IS DISTINCT FROM 'cpu'
 OR r.kind IS DISTINCT FROM 'v2' OR r.graph_sealed IS DISTINCT FROM true OR r.cancel_requested IS DISTINCT FROM false
 OR w.id IS NULL OR w.workflow_type IS DISTINCT FROM 'preview' OR w.renderer_version IS DISTINCT FROM 'v2.2'
 OR w.input_snapshot_id IS DISTINCT FROM r.input_snapshot_id
 OR r.config_snapshot->>'project_id' IS DISTINCT FROM w.project_id::text
 OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i JOIN photo.artifacts a
 ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
 WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='Params'
 AND i.artifact_id=t.params_artifact_id AND i.sha256=params_sha AND a.state='ready'
 AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes=octet_length(p_body)
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())) THEN RETURN NULL;END IF;
 pin:=t.execution_profile->'multi_qualification';
 IF pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'multi_runtime_sha'
 OR coalesce(t.execution_profile->>'multi_runtime_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 FOR binding IN SELECT value FROM jsonb_array_elements(body->'frames') LOOP
 IF photo.v28_local_portrait_source_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
 (r.config_snapshot->'v29_multi'->>'auth_session_id')::uuid,binding) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract='WorkingImage' AND i.artifact_id::text=binding->'source'->>'artifact_id'
 AND i.sha256=binding->'source'->>'sha256') THEN
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN NULL;END IF;
 END LOOP;
 IF pin->'calibration' IS DISTINCT FROM body->'calibration' THEN
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);RETURN NULL;END IF;
 locations:=photo.v29_multi_qualification_sources(p_tenant,w.project_id,r.created_by,
 (r.config_snapshot->'v29_multi'->>'auth_session_id')::uuid,pin,operation);
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF locations IS NULL OR jsonb_array_length(locations) IS DISTINCT FROM 2 THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(locations) item WHERE NOT EXISTS(
 SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
 AND i.contract=item->>'contract' AND i.artifact_id::text=item->>'artifact_id' AND i.sha256=item->>'sha256')) THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.task_results WHERE tenant_id=p_tenant AND id=p_result
 AND task_id=p_task AND attempt=p_attempt AND fencing_token=p_fence AND review_required IS TRUE) THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'operation',operation,'run_id',r.id,'task_id',t.id,'attempt',p_attempt,'fence',p_fence,
 'result_id',p_result,'params_ref',jsonb_build_object('artifact_id',t.params_artifact_id,'sha256',params_sha),
 'params',body,'profile',t.execution_profile,'qualification_objects',locations);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_multi_adoption_evidence(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
