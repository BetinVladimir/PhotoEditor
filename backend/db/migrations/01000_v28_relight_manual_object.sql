-- +goose Up
-- Exact object discovery scoped to the original manual producer. No IO/admission.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_intent_manual_object(p_tenant uuid,p_run uuid,p_mask_body text,p_root_body text,p_protected boolean,p_kind text)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE origin jsonb;ref jsonb;doc jsonb;original jsonb;request jsonb;expected_contract text;a photo.artifacts%ROWTYPE;
BEGIN
 IF p_kind IS NULL OR p_kind NOT IN('root','params','target','guard','vectors') THEN RETURN NULL;END IF;
 origin:=photo.v28_relight_intent_manual_origin(p_tenant,p_run,p_mask_body,p_root_body,p_protected);
 IF origin IS NULL THEN RETURN NULL;END IF;
 IF p_kind IN('root','params') THEN
 ref:=origin->p_kind;expected_contract:=CASE WHEN p_kind='root' THEN 'RasterizedMask' ELSE 'Params' END;
 ELSE
 IF p_root_body IS NULL THEN RETURN NULL;END IF;
 doc:=p_root_body::jsonb;
 IF p_kind IN('target','guard') THEN
 ref:=doc->CASE WHEN p_kind='target' THEN 'target' ELSE 'protected' END->'artifact';expected_contract:='NpyArray';
 ELSE
 -- The interactive row freezes the original external dependency. Params bytes
 -- and exact vector geometry must independently be verified by the reader.
 SELECT i.params INTO original FROM photo.interactive_tasks i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.artifacts r ON r.tenant_id=t.tenant_id AND r.created_by_task_id=t.id
 WHERE r.tenant_id=p_tenant AND r.id=(origin->'root'->>'artifact_id')::uuid
 AND t.params_artifact_id=(origin->'params'->>'artifact_id')::uuid
 AND i.kind='mask_manual' AND i.operation='mask.rasterize' AND i.params_sha256=origin->'params'->>'sha256' FOR SHARE OF i;
 ref:=original->'vectors';expected_contract:='ManualMask';
 IF ref IS NULL OR ref='null'::jsonb OR ref IS DISTINCT FROM doc->'vectors' THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.interactive_tasks i JOIN photo.tasks t ON t.tenant_id=i.tenant_id AND t.id=i.task_id
 JOIN photo.artifacts r ON r.tenant_id=t.tenant_id AND r.created_by_task_id=t.id
 WHERE r.tenant_id=p_tenant AND r.id=(origin->'root'->>'artifact_id')::uuid
 AND i.declared_inputs @> jsonb_build_array(ref) AND t.inputs @> jsonb_build_array(ref)) THEN RETURN NULL;END IF;
 END IF;
 END IF;
 SELECT v28_relight_intent INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements((request->>'inputs_jcs')::jsonb) i WHERE i->'ref'=ref AND i->>'contract'=expected_contract) THEN RETURN NULL;END IF;
 SELECT * INTO a FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(ref->>'artifact_id')::uuid AND sha256=ref->>'sha256' FOR SHARE;
 IF a.id IS NULL OR a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL
 OR a.object_version IS NULL OR a.object_version IN('','null')
 OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())
 OR (expected_contract='NpyArray' AND (a.bytes NOT BETWEEN 1 AND 800065536 OR a.media_type IS DISTINCT FROM 'application/octet-stream'))
 OR (expected_contract<>'NpyArray' AND (a.bytes NOT BETWEEN 2 AND 1048576 OR a.media_type IS DISTINCT FROM 'application/json')) THEN RETURN NULL;END IF;
 IF photo.v28_relight_intent_manual_origin(p_tenant,p_run,p_mask_body,p_root_body,p_protected) IS DISTINCT FROM origin THEN RETURN NULL;END IF;
 RETURN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'object_key',a.object_key,'object_version',a.object_version,'bytes',a.bytes,'media_type',a.media_type);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v28_relight_intent_manual_object(uuid,uuid,text,text,boolean,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_intent_manual_object(uuid,uuid,text,text,boolean,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_intent_manual_object(uuid,uuid,text,text,boolean,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
