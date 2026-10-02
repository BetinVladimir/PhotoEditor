-- +goose Up
-- Exact retry acknowledgement only; never mutates or activates sealed inputs.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_lens_registered_inputs(p_tenant uuid,p_run uuid,p_task uuid,
 p_params uuid,p_params_sha text,p_manifest jsonb,p_chunks jsonb,p_count integer,p_entries_sha text,p_direct jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(SELECT 1 FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task
 AND t.operation='optical.lens' AND t.operation_version='1' AND t.resource_class='cpu'
 AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
 AND t.params_artifact_id=p_params
 AND photo.v25_lens_document_scope(p_tenant,p_run,p_task,'v25_lens_params',p_params_sha)
 AND photo.v2_input_closure_ready(t.id)
 AND t.execution_profile->'v2_input_manifest'=p_manifest
 AND t.execution_profile->>'v2_entry_count'=p_count::text
 AND t.execution_profile->>'v2_entries_sha'=p_entries_sha
 AND t.inputs=jsonb_build_array(p_manifest)||p_direct
 AND p_count BETWEEN 1 AND 1000000 AND jsonb_typeof(p_chunks)='array'
 AND jsonb_array_length(p_chunks)=(p_count+511)/512
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(p_chunks) WITH ORDINALITY expected(ref,ordinal)
 WHERE NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id
 AND i.ordinal=1000000+expected.ordinal-1 AND i.contract='InputClosureChunk'
 AND i.artifact_id::text=expected.ref->>'artifact_id' AND i.sha256=expected.ref->>'sha256')));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_lens_registered_inputs(uuid,uuid,uuid,uuid,text,jsonb,jsonb,integer,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_lens_registered_inputs(uuid,uuid,uuid,uuid,text,jsonb,jsonb,integer,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_lens_registered_inputs(uuid,uuid,uuid,uuid,text,jsonb,jsonb,integer,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
