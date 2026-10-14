-- +goose Up
-- A leased import worker may observe only its own current prepared person-mask
-- grant. Reuse the complete worker admission before reading the saved parsed
-- source; no archive confirmation or hash equality alone grants authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_worker_person_mask_live(
 p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text,
 p_workflow uuid,p_project uuid,p_target_version uuid,p_prepared_mask uuid,p_mask_sha text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope jsonb;parsed text;result_epoch bigint;
BEGIN
 IF p_workflow IS NULL OR p_project IS NULL OR p_target_version IS NULL OR p_prepared_mask IS NULL
 OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 scope:=photo.v30_interchange_import_ready(p_tenant,p_run,p_task,p_attempt,p_fence,
  p_worker,p_params,p_sha,p_json,p_digest);
 IF scope IS NULL OR scope->>'workflow_id' IS DISTINCT FROM p_workflow::text
 OR scope->>'project_id' IS DISTINCT FROM p_project::text THEN RETURN NULL;END IF;
 SELECT e.v30_import_parsed_jcs INTO parsed FROM photo.backend_documents e
 WHERE e.tenant_id=p_tenant AND e.run_id=p_run AND e.owner_id=p_task
 AND e.purpose='v30_interchange_import_evidence'
 AND e.artifact_id=photo.v30_uuid5(p_task,'interchange:evidence:1')
 AND e.state='ready' FOR SHARE;
 IF parsed IS NULL THEN RETURN NULL;END IF;
 result_epoch:=photo.v30_import_person_mask_prepared_live(p_tenant,
  p_workflow,p_target_version,p_prepared_mask,p_mask_sha,parsed);
 RETURN result_epoch;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
ALTER FUNCTION photo.v30_import_worker_person_mask_live(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_worker_person_mask_live(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_import_worker_person_mask_live(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,uuid,uuid,uuid,text)
 TO photo_worker;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
