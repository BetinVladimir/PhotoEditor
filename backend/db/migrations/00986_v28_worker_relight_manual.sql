-- +goose Up
-- Discover the canonical manual root; supplied root bytes trigger full child checks.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_worker_relight_manual_origin(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text,
 p_mask_body text,p_root_body text,p_protected boolean)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected photo.artifacts%ROWTYPE;producer photo.tasks%ROWTYPE;result photo.task_results%ROWTYPE;
 root photo.artifacts%ROWTYPE;original photo.artifacts%ROWTYPE;binding jsonb;doc jsonb;child jsonb;role text;scope record;
BEGIN
 IF p_protected IS NULL OR photo.v28_worker_relight_source_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true
 OR p_mask_body IS NULL OR octet_length(p_mask_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 binding:=p_body::jsonb->CASE WHEN p_protected THEN 'protected' ELSE 'subject' END;
 SELECT a.* INTO selected FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND i.task_id=p_task AND a.id=(binding->>'artifact_id')::uuid AND a.sha256=binding->>'sha256' FOR SHARE OF a;
 IF selected.id IS NULL OR selected.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_mask_body,'UTF8')),'hex')
 OR selected.bytes IS DISTINCT FROM octet_length(p_mask_body)::bigint
 OR selected.output_role IS DISTINCT FROM (CASE WHEN p_protected THEN 'mask_guard_ref' ELSE 'mask_target_ref' END)
 OR selected.state IS DISTINCT FROM 'ready' OR selected.verified_at IS NULL OR selected.media_type IS DISTINCT FROM 'application/json'
 OR selected.object_version IS NULL OR selected.object_version IN('','null')
 OR (selected.expires_at IS NOT NULL AND selected.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 SELECT w.project_id,w.input_snapshot_id,r.created_by,(r.config_snapshot->'v28_relight'->>'auth_session_id')::uuid AS session_id INTO scope
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id WHERE r.tenant_id=p_tenant AND r.id=p_run;
 IF photo.v28_manual_mask_document_scope(p_tenant,scope.project_id,scope.input_snapshot_id,scope.created_by,scope.session_id,p_body::jsonb->'base',
 selected.id,selected.sha256,(p_mask_body::jsonb->'artifact'->>'artifact_id')::uuid,p_mask_body::jsonb->'artifact'->>'sha256',p_protected) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=selected.created_by_task_id FOR SHARE;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id FOR SHARE;
 IF producer.id IS NULL OR producer.operation IS DISTINCT FROM 'mask.rasterize' OR producer.operation_version IS DISTINCT FROM '1'
 OR producer.state IS DISTINCT FROM 'succeeded' OR result.id IS NULL OR result.task_id IS DISTINCT FROM producer.id
 OR result.attempt IS DISTINCT FROM producer.attempt OR result.fencing_token IS DISTINCT FROM producer.fencing_token
 OR selected.produced_attempt IS DISTINCT FROM result.attempt OR selected.produced_fence IS DISTINCT FROM result.fencing_token
 OR NOT result.outputs @> jsonb_build_array(binding)
 OR NOT EXISTS(SELECT 1 FROM photo.task_attempts a WHERE a.tenant_id=p_tenant AND a.task_id=producer.id AND a.attempt=result.attempt AND a.fencing_token=result.fencing_token AND a.finished_at IS NOT NULL) THEN RETURN NULL;END IF;
 SELECT a.* INTO root FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND i.task_id=p_task AND a.created_by_task_id=producer.id AND a.output_role='rasterized_mask'
 AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)) FOR SHARE OF a;
 IF root.id IS NULL THEN RETURN NULL;END IF;
 SELECT a.* INTO original FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND i.task_id=p_task AND a.id=producer.params_artifact_id AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF original.id IS NULL THEN RETURN NULL;END IF;
 IF p_root_body IS NOT NULL THEN
 IF octet_length(p_root_body) IS DISTINCT FROM root.bytes OR root.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_root_body,'UTF8')),'hex') THEN RETURN NULL;END IF;
 doc:=p_root_body::jsonb;
 IF doc->(CASE WHEN p_protected THEN 'protected' ELSE 'target' END) IS DISTINCT FROM p_mask_body::jsonb THEN RETURN NULL;END IF;
 FOR child,role IN SELECT doc->'target'->'artifact','mask_target'::text UNION ALL SELECT doc->'protected'->'artifact','mask_protected'::text LOOP
 IF child IS NULL OR NOT EXISTS(SELECT 1 FROM photo.artifacts a JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.sha256=a.sha256
 WHERE a.tenant_id=p_tenant AND i.task_id=p_task AND a.id=(child->>'artifact_id')::uuid AND a.sha256=child->>'sha256'
 AND a.created_by_task_id=producer.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token AND a.output_role=role
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND result.outputs @> jsonb_build_array(child)) THEN RETURN NULL;END IF;
 END LOOP;
 END IF;
 IF photo.v28_worker_relight_source_ready(p_tenant,p_run,p_task,p_attempt,p_fence,p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('root',jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),'params',jsonb_build_object('artifact_id',original.id,'sha256',original.sha256));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_worker_relight_manual_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text,boolean) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_worker_relight_manual_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text,boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_worker_relight_manual_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,text,text,boolean) TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
