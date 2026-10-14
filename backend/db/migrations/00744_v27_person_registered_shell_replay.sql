-- +goose Up
-- Native LRASPP registered-shell replay preserves the original exact shell matcher.
-- Existing defect/transfer and other workflow replay use the prior wrapper.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) RENAME TO v2_pending_shell_matches_pre_v27_person;
REVOKE ALL ON FUNCTION photo.v2_pending_shell_matches_pre_v27_person(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v2_pending_shell_matches(p_tenant uuid,p_run uuid,p_workflow uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_type text,p_policy_sha text,p_config_sha text,p_config jsonb,p_tasks jsonb,p_edges jsonb,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; profile jsonb; amended jsonb:='[]';task uuid;
BEGIN
 IF p_type IS DISTINCT FROM 'semantic' OR NOT (p_config ? 'v27_person_prepare') THEN
  RETURN photo.v2_pending_shell_matches_pre_v27_person(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,p_tasks,p_edges,p_bytes);
 END IF;
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' OR jsonb_array_length(p_tasks)<>1
 OR jsonb_typeof(p_config->'v27_person_prepare') IS DISTINCT FROM 'object'
 OR p_config->'v27_person_prepare'->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR p_config ? 'v27_defect_prepare' OR p_config ? 'v27_detector_prepare'
 OR p_run IS DISTINCT FROM photo.semantic_uuid_v5((p_config->'v27_person_prepare'->>'parent_run_id')::uuid,'semantic.mask.person:reference')
 OR jsonb_array_length(p_edges)<>0 THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  task:=(item->>'id')::uuid;
  SELECT execution_profile INTO profile FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task;
  IF profile ? 'v2_input_manifest' THEN
   IF item->>'operation' IS DISTINCT FROM 'mask.person'
   OR item->>'operation_version' IS DISTINCT FROM '1' OR item->>'resource_class' IS DISTINCT FROM 'gpu'
   OR p_config->'v27_person_prepare'->>'task_id' IS DISTINCT FROM task::text
   OR p_config->'v27_person_prepare'->>'input_digest' IS DISTINCT FROM item->>'input_digest'
   OR p_config->'v27_person_prepare'->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',item->>'params_artifact_id','sha256',item->>'params_sha')
   OR (item->'execution_profile') IS DISTINCT FROM (profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
   OR NOT photo.v2_input_closure_ready(task) THEN RETURN false;END IF;
   item:=jsonb_set(item,'{execution_profile}',profile);
  END IF;
  amended:=amended||jsonb_build_array(item);
 END LOOP;
 RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,amended,p_edges,p_bytes);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
