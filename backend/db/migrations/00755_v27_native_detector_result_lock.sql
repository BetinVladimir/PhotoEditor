-- +goose Up
-- Final canonical SFD/object barrier for downstream native task admission.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_detector_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;locked_r photo.runs%ROWTYPE;c jsonb;found_result record;actual jsonb;files jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object' OR octet_length(p_expected::text)>16384
 OR (p_expected-'schema_version'-'result_id'-'output'-'params'-'files')<>'{}'
 OR p_expected->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference');
 IF NOT FOUND THEN RETURN false;END IF;
 c:=r.config_snapshot->'v27_detector_prepare';
 IF photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,
 (c->'source'->>'asset_version_id')::uuid,(c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256') IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO locked_r FROM photo.runs WHERE tenant_id=p_tenant AND id=r.id FOR SHARE;
 IF NOT FOUND OR locked_r.config_snapshot IS DISTINCT FROM r.config_snapshot THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=r.id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM run_id FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id FOR SHARE;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY attempt FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (created_by_task_id=p_task OR id IN(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) ORDER BY id FOR SHARE;
 PERFORM artifact_id FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY artifact_id FOR SHARE;
 SELECT * INTO found_result FROM photo.semantic_native_detector_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('role',refs.role,'artifact_id',a.id,'sha256',a.sha256,'bucket',a.bucket,'key',a.object_key,'version',a.object_version,'bytes',a.bytes,'media_type',a.media_type) ORDER BY refs.role) INTO files
 FROM (VALUES('face_boxes',(found_result.output->>'artifact_id')::uuid),('detector_params',(found_result.params->>'artifact_id')::uuid)) refs(role,id)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=refs.id;
 actual:=jsonb_build_object('schema_version',1,'result_id',found_result.result_id,'output',found_result.output,'params',found_result.params,'files',files);
 RETURN actual=p_expected;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_detector_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_detector_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_detector_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
