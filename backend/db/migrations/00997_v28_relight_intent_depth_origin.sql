-- +goose Up
-- Scheduler discovery of original depth Params for a frozen preliminary intent.
-- Returned metadata is not proof of original Params bytes or task admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_intent_depth_origin(p_tenant uuid,p_run uuid,p_depth_body text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;params jsonb;inputs jsonb;depth jsonb;root photo.artifacts%ROWTYPE;
 producer photo.tasks%ROWTYPE;original photo.artifacts%ROWTYPE;result photo.task_results%ROWTYPE;ref jsonb;role text;child uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_depth_body IS NULL OR octet_length(p_depth_body) NOT BETWEEN 2 AND 1048576 THEN RETURN NULL;END IF;
 SELECT v28_relight_intent INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF request IS NULL OR photo.save_v28_relight_intent(p_tenant,p_run,request) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 params:=(request->>'params_jcs')::jsonb;inputs:=(request->>'inputs_jcs')::jsonb;
 depth:=p_depth_body::jsonb;
 SELECT * INTO root FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(params->'depth'->>'artifact_id')::uuid FOR SHARE;
 IF root.id IS NULL OR root.sha256 IS DISTINCT FROM params->'depth'->>'sha256'
 OR root.sha256 IS DISTINCT FROM encode(sha256(convert_to(p_depth_body,'UTF8')),'hex')
 OR root.bytes IS DISTINCT FROM octet_length(p_depth_body)::bigint
 OR root.output_role IS DISTINCT FROM 'relative_depth_ref' OR root.media_type IS DISTINCT FROM 'application/json'
 OR root.state IS DISTINCT FROM 'ready' OR root.verified_at IS NULL
 OR root.object_version IS NULL OR root.object_version IN('','null')
 OR (root.expires_at IS NOT NULL AND root.expires_at<=clock_timestamp()) THEN RETURN NULL;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=(SELECT run_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=root.created_by_task_id) AND NOT cancel_requested FOR SHARE;
 IF NOT FOUND THEN RETURN NULL;END IF;
 SELECT * INTO producer FROM photo.tasks WHERE tenant_id=p_tenant AND id=root.created_by_task_id FOR SHARE;
 SELECT * INTO result FROM photo.task_results WHERE tenant_id=p_tenant AND id=producer.canonical_result_id FOR SHARE;
 IF producer.id IS NULL OR producer.operation IS DISTINCT FROM 'depth.predict' OR producer.operation_version IS DISTINCT FROM '1'
 OR producer.state IS DISTINCT FROM 'succeeded' OR result.id IS NULL OR result.task_id IS DISTINCT FROM producer.id
 OR result.attempt IS DISTINCT FROM producer.attempt OR result.fencing_token IS DISTINCT FROM producer.fencing_token
 OR root.produced_attempt IS DISTINCT FROM result.attempt OR root.produced_fence IS DISTINCT FROM result.fencing_token
 OR NOT result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
 OR NOT EXISTS(SELECT 1 FROM photo.task_attempts a WHERE a.tenant_id=p_tenant AND a.task_id=producer.id
 AND a.attempt=result.attempt AND a.fencing_token=result.fencing_token AND a.finished_at IS NOT NULL)
 THEN RETURN NULL;END IF;
 FOR ref,role IN SELECT depth->'artifact','relative_depth'::text UNION ALL
 SELECT depth->'invalid_mask','depth_invalid'::text WHERE depth->'invalid_mask' IS NOT NULL AND depth->'invalid_mask'<>'null'::jsonb LOOP
 SELECT a.id INTO child FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=(ref->>'artifact_id')::uuid AND a.sha256=ref->>'sha256'
 AND a.created_by_task_id=producer.id AND a.produced_attempt=result.attempt AND a.produced_fence=result.fencing_token
 AND a.output_role=role AND a.state='ready' AND a.verified_at IS NOT NULL AND a.bytes>0
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND result.outputs @> jsonb_build_array(ref) FOR SHARE;
 IF child IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) i WHERE i->'ref'=ref AND i->>'contract'='NpyArray') THEN RETURN NULL;END IF;
 END LOOP;
 IF depth->'artifact' IS NULL OR depth->'artifact'='null'::jsonb THEN RETURN NULL;END IF;
 SELECT a.* INTO original FROM photo.artifacts a
 WHERE a.tenant_id=p_tenant AND a.id=producer.params_artifact_id
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 65536
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) FOR SHARE OF a;
 IF original.id IS NULL OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(inputs) i WHERE i->'ref'=params->'depth' AND i->>'contract'='RelativeDepth')
 OR NOT EXISTS(SELECT 1 FROM photo.runs pr JOIN photo.snapshot_items si ON si.tenant_id=pr.tenant_id AND si.snapshot_id=pr.input_snapshot_id
 WHERE pr.tenant_id=p_tenant AND pr.id=producer.run_id AND NOT pr.cancel_requested
 AND si.asset_version_id=(params->'base'->>'asset_version_id')::uuid
 AND si.artifact_id=(params->'base'->'source'->>'artifact_id')::uuid AND si.sha256=params->'base'->'source'->>'sha256')
 THEN RETURN NULL;END IF;
 IF photo.save_v28_relight_intent(p_tenant,p_run,request) IS DISTINCT FROM true THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',original.id,'sha256',original.sha256,'bucket',original.bucket,
 'object_key',original.object_key,'object_version',original.object_version,'bytes',original.bytes,'media_type',original.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_intent_depth_origin(uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_intent_depth_origin(uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_intent_depth_origin(uuid,uuid,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
