-- +goose Up
-- Recovery/staging must use the exact dedicated task-owned Params intent.
-- +goose StatementBegin
ALTER FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) RENAME TO save_v28_donor_descriptor_pre_params_owner;
REVOKE ALL ON FUNCTION photo.save_v28_donor_descriptor_pre_params_owner(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.save_v28_donor_descriptor(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_descriptor text,p_params text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE request jsonb;plan jsonb;task jsonb;
BEGIN
 IF p_descriptor IS NULL OR octet_length(p_descriptor) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT v28_donor_params_intent INTO request FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR request IS NULL THEN RETURN false;END IF;
 plan:=p_descriptor::jsonb;task:=plan->'task';
 IF request->>'actor_id' IS DISTINCT FROM p_actor::text OR request->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR request->>'owner_id' IS DISTINCT FROM task->>'id' OR request->>'node_key' IS DISTINCT FROM task->>'node_key'
 OR request->>'params_jcs' IS DISTINCT FROM p_params OR request->>'params_sha' IS DISTINCT FROM task->>'params_sha'
 OR request->'qualification' IS DISTINCT FROM task->'execution_profile'->'donor_qualification'
 OR request->'qualification_objects' IS DISTINCT FROM plan->'qualification_objects'
 OR request->'reserved_bytes' IS DISTINCT FROM plan->'reserved_bytes'
 OR photo.v28_donor_params_document_scope(p_tenant,p_run,(task->>'id')::uuid,'task_params',task->>'params_sha',octet_length(p_params)) IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM artifact_id FROM photo.backend_documents WHERE tenant_id=p_tenant AND run_id=p_run
 AND owner_id=(task->>'id')::uuid AND purpose='task_params' AND state='ready'
 AND artifact_id=(task->>'params_artifact_id')::uuid AND sha256=task->>'params_sha' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN photo.save_v28_donor_descriptor_pre_params_owner(p_tenant,p_run,p_actor,p_session,p_descriptor,p_params);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.save_v28_donor_descriptor(uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
