-- +goose Up
-- Final native canonical output lock; model/input qualification and parent admission remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_native_result(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_origin jsonb,p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_result record; png photo.artifacts%ROWTYPE; actual jsonb;
BEGIN
 IF jsonb_typeof(p_expected) IS DISTINCT FROM 'object' OR octet_length(p_expected::text)>16384
 OR (p_expected-'result_id'-'outputs'-'params'-'png')<>'{}'
 OR photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR photo.lock_semantic_defect_origin_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_origin) IS NOT TRUE THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM artifact_id FROM photo.output_uploads WHERE tenant_id=p_tenant AND task_id=p_task ORDER BY artifact_id FOR SHARE;
 SELECT * INTO png FROM photo.artifacts WHERE tenant_id=p_tenant AND id=(p_expected->'png'->>'artifact_id')::uuid FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO current_result FROM photo.semantic_defect_native_result_catalog(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task);
 IF NOT FOUND THEN RETURN false;END IF;
 actual:=jsonb_build_object('result_id',current_result.result_id,'outputs',current_result.outputs,'params',current_result.params,
 'png',jsonb_build_object('artifact_id',png.id,'sha256',png.sha256,'bucket',png.bucket,'key',png.object_key,'version',png.object_version,'bytes',png.bytes,'media_type',png.media_type));
 RETURN actual=p_expected AND current_result.params=(p_origin->0)-'role'-'bucket'-'key'-'version'-'bytes'-'media_type'
 AND current_result.outputs->3=(p_origin->1)-'role'-'bucket'-'key'-'version'-'bytes'-'media_type'
 AND current_result.outputs->4=(p_origin->2)-'role'-'bucket'-'key'-'version'-'bytes'-'media_type'
 AND current_result.outputs->0=(p_origin->3)-'role'-'bucket'-'key'-'version'-'bytes'-'media_type'
 AND current_result.outputs->1=(p_origin->4)-'role'-'bucket'-'key'-'version'-'bytes'-'media_type'
 AND current_result.outputs->2=jsonb_build_object('artifact_id',png.id,'sha256',png.sha256);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_native_result(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_native_result(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_native_result(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
