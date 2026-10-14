-- +goose Up
-- Locks exact completed native defect dependency; byte proof is verified before SQL.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_target_defect_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_task uuid,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE admission jsonb;face jsonb;r photo.runs%ROWTYPE;found_result record;files jsonb;actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object' OR octet_length(p_expected::text)>33554432
 OR (p_expected-ARRAY['schema_version','admission','result_id','outputs','params','files'])<>'{}'
 OR p_expected->'schema_version' IS DISTINCT FROM '1'::jsonb
 THEN RETURN false;END IF;
 admission:=p_expected->'admission';face:=admission->'landmarks'->'face';
 IF face->>'tenant_id' IS DISTINCT FROM p_tenant::text OR face->>'project_id' IS DISTINCT FROM p_project::text
 OR face->>'actor_id' IS DISTINCT FROM p_actor::text OR face->>'session_id' IS DISTINCT FROM p_session::text
 OR face->>'parent_run_id' IS DISTINCT FROM p_parent::text OR face->>'request_sha' IS DISTINCT FROM p_request_sha THEN RETURN false;END IF;
 IF photo.lock_semantic_target_defect_sealed_evidence(p_tenant,photo.semantic_uuid_v5(((admission->>'config_text')::jsonb->'v27_target_defect_prepare'->>'target_namespace')::uuid,'semantic.defect.prepare:reference'),admission) IS NOT TRUE THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=photo.semantic_uuid_v5(((admission->>'config_text')::jsonb->'v27_target_defect_prepare'->>'target_namespace')::uuid,'semantic.defect.prepare:reference') FOR SHARE;
 IF NOT FOUND OR r.status<>'completed' THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND run_id=r.id FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM task_id FROM photo.task_attempts WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY attempt FOR SHARE;
 PERFORM id FROM photo.task_results WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY id FOR SHARE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND (created_by_task_id=p_task OR id IN(SELECT params_artifact_id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task)) ORDER BY id FOR SHARE;
 PERFORM artifact_id FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY artifact_id FOR SHARE;
 SELECT * INTO found_result FROM photo.semantic_target_defect_result_catalog(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task);
 IF NOT FOUND OR found_result.result_id::text IS DISTINCT FROM p_expected->>'result_id' THEN RETURN false;END IF;
 SELECT jsonb_agg(jsonb_build_object('role',(ARRAY['defect_labels','defect_protection','defect_labels_png','defect_candidates','defect_propose_report','params'])[o.ordinal+1],
 'ref',jsonb_build_object('artifact_id',o.artifact_id,'sha256',o.sha256),'bucket',o.bucket,'key',o.object_key,'version',o.object_version,'bytes',o.bytes,'media',o.media_type) ORDER BY o.ordinal)
 INTO files FROM photo.semantic_target_defect_result_objects(p_tenant,p_project,p_actor,p_session,p_parent,p_request_sha,p_task,found_result.result_id) o;
 IF jsonb_array_length(files) IS DISTINCT FROM 6 THEN RETURN false;END IF;
 actual:=jsonb_build_object('schema_version',1,'admission',admission,'result_id',found_result.result_id,'outputs',found_result.outputs,'params',found_result.params,'files',files);
 RETURN actual=p_expected;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_target_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_target_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_target_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
