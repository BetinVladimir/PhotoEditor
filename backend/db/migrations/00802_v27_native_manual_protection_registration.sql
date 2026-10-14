-- +goose Up
-- Complete manual result authority precedes immutable native root registration.
-- +goose StatementBegin
CREATE FUNCTION photo.register_semantic_native_manual_input_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_document uuid,p_root uuid,p_root_sha text,p_body text,p_report text,p_completed jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native_completed jsonb;
BEGIN
 IF photo.lock_semantic_native_manual_result(p_tenant,p_project,p_actor,p_session,(p_anchor->>'parent_run_id')::uuid,p_anchor->>'request_sha',p_task,p_completed) IS NOT TRUE THEN RETURN false;END IF;
 -- Manual authority is held before delegating exact insert-only root provenance.
 native_completed:=jsonb_set(p_completed,'{admission}',p_completed->'admission'->'native',false);
 RETURN photo.register_semantic_native_input_protection(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task,p_result,p_anchor,p_document,p_root,p_root_sha,p_body,p_report,native_completed) IS TRUE;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.register_semantic_native_manual_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.register_semantic_native_manual_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.register_semantic_native_manual_input_protection(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,uuid,text,text,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
