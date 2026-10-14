-- +goose Up
-- Hold full manual authority before exact registered-root evidence locks.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_manual_input_protection_evidence(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_root uuid,p_root_sha text,p_expected jsonb,p_completed jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native_completed jsonb;
BEGIN
 IF photo.lock_semantic_native_manual_result(p_tenant,p_project,p_actor,p_session,(p_anchor->>'parent_run_id')::uuid,p_anchor->>'request_sha',p_task,p_completed) IS NOT TRUE THEN RETURN false;END IF;
 native_completed:=jsonb_set(p_completed,'{admission}',p_completed->'admission'->'native',false);
 RETURN photo.lock_semantic_native_input_protection_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task,p_result,p_anchor,p_root,p_root_sha,p_expected,native_completed) IS TRUE;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_native_manual_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_manual_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_manual_input_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
