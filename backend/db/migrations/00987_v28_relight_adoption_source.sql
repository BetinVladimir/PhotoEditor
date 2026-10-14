-- +goose Up
-- Completed-task source/qualification gate only; producer replay and commit separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_adoption_source(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE context record;scope record;t photo.tasks%ROWTYPE;r photo.runs%ROWTYPE;
 params photo.artifacts%ROWTYPE;root photo.artifacts%ROWTYPE;pin jsonb;base jsonb;previous_tenant text;qualified boolean;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR SHARE;
 SELECT * INTO context FROM photo.v2_adoption_context(p_tenant,p_task);
 IF NOT FOUND OR context.attempt IS DISTINCT FROM p_attempt OR context.fence IS DISTINCT FROM p_fence
 OR context.result_id IS DISTINCT FROM p_result OR context.operation IS DISTINCT FROM 'image.relight'
 OR context.operation_version IS DISTINCT FROM '1' OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 65536 THEN RETURN NULL;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR SHARE;
 IF NOT r.graph_sealed OR t.resource_class IS DISTINCT FROM 'cpu' OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
 OR t.execution_profile->>'relight_runtime_sha' IS NULL OR t.execution_profile->>'relight_runtime_sha' !~ '^[0-9a-f]{64}$'
 OR photo.v2_input_closure_ready(p_task) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.task_attempts a WHERE a.tenant_id=p_tenant AND a.task_id=p_task AND a.attempt=p_attempt AND a.fencing_token=p_fence AND a.finished_at IS NOT NULL) THEN RETURN NULL;END IF;
 SELECT * INTO params FROM photo.artifacts WHERE tenant_id=p_tenant AND id=t.params_artifact_id FOR SHARE;
 IF params.id IS NULL OR params.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_body,'UTF8')),'hex')
 OR params.bytes IS DISTINCT FROM octet_length(p_body)::bigint OR params.media_type IS DISTINCT FROM 'application/json'
 OR params.state IS DISTINCT FROM 'ready' OR params.verified_at IS NULL OR params.object_version IS NULL OR params.object_version IN('','null')
 OR (params.expires_at IS NOT NULL AND params.expires_at<=clock_timestamp())
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.artifact_id=params.id AND i.sha256=params.sha256 AND i.contract='Params') THEN RETURN NULL;END IF;
 SELECT * INTO root FROM photo.artifacts WHERE tenant_id=p_tenant AND id=context.root_artifact_id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.output_uploads u WHERE u.tenant_id=p_tenant AND u.task_id=p_task
 AND u.artifact_id=root.id AND u.attempt=p_attempt AND u.fence=p_fence AND u.role='document' AND u.state='ready'
 AND u.sha256=root.sha256 AND u.bucket=root.bucket AND u.object_key=root.object_key AND u.object_version=root.object_version AND u.bytes=root.bytes) THEN RETURN NULL;END IF;
 IF root.id IS NULL OR root.sha256 IS DISTINCT FROM context.root_sha256 OR root.created_by_task_id IS DISTINCT FROM p_task
 OR root.produced_attempt IS DISTINCT FROM p_attempt OR root.produced_fence IS DISTINCT FROM p_fence OR root.output_role IS DISTINCT FROM 'document'
 OR root.state IS DISTINCT FROM 'ready' OR root.verified_at IS NULL OR root.media_type IS DISTINCT FROM 'application/json'
 OR root.object_version IS NULL OR root.object_version IN('','null') OR root.bytes NOT BETWEEN 2 AND 8388608
 OR (root.expires_at IS NOT NULL AND root.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 base:=p_body::jsonb->'base';pin:=t.execution_profile->'relight_qualification';
 SELECT w.project_id,w.input_snapshot_id,r.created_by,(r.config_snapshot->'v28_relight'->>'auth_session_id')::uuid AS session_id INTO scope
 FROM photo.v2_workflows w WHERE w.tenant_id=p_tenant AND w.run_id=r.id;
 PERFORM v.id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
 JOIN photo.artifacts s ON s.tenant_id=v.tenant_id AND s.id=v.master_artifact_id
 WHERE v.tenant_id=p_tenant AND v.id=(base->>'asset_version_id')::uuid FOR SHARE OF v,a,s;
 PERFORM id FROM photo.snapshots WHERE tenant_id=p_tenant AND id=scope.input_snapshot_id FOR SHARE;
 PERFORM id FROM photo.sessions WHERE id=scope.session_id FOR SHARE;
 IF photo.v28_local_portrait_source_scope(p_tenant,scope.project_id,scope.input_snapshot_id,scope.created_by,scope.session_id,base) IS DISTINCT FROM true
 OR pin->'calibration' IS DISTINCT FROM p_body::jsonb->'calibration'
 OR pin->>'runtime_lock_sha' IS DISTINCT FROM t.execution_profile->>'relight_runtime_sha'
 OR NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract='WorkingImage' AND i.artifact_id::text=base->'source'->>'artifact_id' AND i.sha256=base->'source'->>'sha256') THEN RETURN NULL;END IF;
 IF EXISTS(SELECT 1 FROM (VALUES('Report'::text,pin->'report'),('Calibration'::text,pin->'calibration')) refs(contract,ref)
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND i.contract=refs.contract AND i.artifact_id::text=refs.ref->>'artifact_id' AND i.sha256=refs.ref->>'sha256')) THEN RETURN NULL;END IF;
 previous_tenant:=current_setting('app.tenant_id',true);PERFORM set_config('app.tenant_id',p_tenant::text,true);
 qualified:=photo.lock_v28_relight_qualification(p_tenant,scope.project_id,scope.created_by,scope.session_id,pin);
 PERFORM set_config('app.tenant_id',coalesce(previous_tenant,''),true);
 IF qualified IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('schema_version',1,'run_id',r.id,'task_id',p_task,'attempt',p_attempt,'fence',p_fence,'result_id',p_result,
 'params_ref',jsonb_build_object('artifact_id',params.id,'sha256',params.sha256),
 'root_ref',jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),'profile',t.execution_profile,'qualification',pin);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN
 IF previous_tenant IS NOT NULL THEN PERFORM set_config('app.tenant_id',previous_tenant,true);END IF;
 RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_adoption_source(uuid,uuid,integer,bigint,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_adoption_source(uuid,uuid,integer,bigint,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_adoption_source(uuid,uuid,integer,bigint,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
