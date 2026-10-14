-- +goose Up
-- Hold exact registered root/upload/original NPY metadata through caller admission.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_input_protection_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_root uuid,p_root_sha text,p_expected jsonb,p_completed jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE actual jsonb;
BEGIN
 IF jsonb_typeof(p_expected) IS DISTINCT FROM 'array' OR octet_length(p_expected::text)>16384 THEN RETURN false;END IF;
 IF jsonb_array_length(p_expected)<>2
 OR photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR photo.lock_semantic_native_defect_result(p_tenant,p_project,p_actor,p_session,(p_anchor->>'parent_run_id')::uuid,p_anchor->>'request_sha',p_task,p_completed) IS NOT TRUE THEN RETURN false;END IF;
 PERFORM document_id FROM photo.semantic_native_protections WHERE tenant_id=p_tenant AND root_id=p_root AND root_sha=p_root_sha AND task_id=p_task AND result_id=p_result FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id IN(p_root,(p_expected->1->>'artifact_id')::uuid) ORDER BY id FOR SHARE;
 PERFORM document_id FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND artifact_id=p_root ORDER BY document_id FOR SHARE;
 SELECT jsonb_agg(jsonb_build_object('ordinal',o.ordinal,'artifact_id',o.artifact_id,'sha256',o.sha256,'bucket',o.bucket,'key',o.object_key,'version',o.object_version,'bytes',o.bytes,'media_type',o.media_type) ORDER BY o.ordinal)
 INTO actual FROM photo.semantic_native_input_protection_objects(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task,p_result,p_anchor,p_root,p_root_sha) o;
 RETURN coalesce(actual=p_expected AND jsonb_array_length(actual)=2,false);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
