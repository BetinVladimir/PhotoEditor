-- +goose Up
-- Preserve strict generic shell matching. V27 input registration adds only
-- verified transport metadata to the frozen execution profile.
-- +goose StatementBegin
ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) RENAME TO v2_pending_shell_matches_pre_v27_registered;
REVOKE ALL ON FUNCTION photo.v2_pending_shell_matches_pre_v27_registered(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.v2_pending_shell_matches(p_tenant uuid,p_run uuid,p_workflow uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_type text,p_policy_sha text,p_config_sha text,p_config jsonb,p_tasks jsonb,p_edges jsonb,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE item jsonb; profile jsonb; amended jsonb:='[]';task uuid;
BEGIN
 IF p_type IS DISTINCT FROM 'semantic' THEN
  RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,p_tasks,p_edges,p_bytes);
 END IF;
 IF jsonb_typeof(p_tasks) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(p_tasks) LOOP
  task:=(item->>'id')::uuid;
  SELECT execution_profile INTO profile FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=task;
  IF profile ? 'v2_input_manifest' THEN
   IF item->>'operation' IS DISTINCT FROM 'semantic.transfer'
   OR (item->'execution_profile') IS DISTINCT FROM (profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
   OR NOT photo.v2_input_closure_ready(task) THEN RETURN false;END IF;
   item:=jsonb_set(item,'{execution_profile}',profile);
  END IF;
  amended:=amended||jsonb_build_array(item);
 END LOOP;
 RETURN photo.v2_pending_shell_matches_pre_v27_registered(p_tenant,p_run,p_workflow,p_project,p_snapshot,p_actor,p_type,p_policy_sha,p_config_sha,p_config,amended,p_edges,p_bytes);
END $$;
ALTER FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
