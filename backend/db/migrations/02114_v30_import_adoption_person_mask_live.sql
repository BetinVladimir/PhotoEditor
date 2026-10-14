-- +goose Up
-- A completed worker lease cannot authorize a later scheduler adoption.
-- Rebind the live prepared-mask grant to the exact canonical result and the
-- saved parsed source; the existing predicate also checks person origin.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_person_mask_live(
 p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_target_version uuid,p_prepared_mask uuid,p_mask_sha text)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;task_run uuid;parsed text;epoch bigint;
BEGIN
 IF p_target_version IS NULL OR p_prepared_mask IS NULL
 OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN NULL;END IF;
 authority:=photo.v30_interchange_import_adoption_context(
  p_tenant,p_task,p_attempt,p_fence,p_result);
 IF authority IS NULL THEN RETURN NULL;END IF;
 SELECT t.run_id INTO task_run FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.id=p_task FOR SHARE;
 IF task_run IS NULL THEN RETURN NULL;END IF;
 SELECT e.v30_import_parsed_jcs INTO parsed FROM photo.backend_documents e
 WHERE e.tenant_id=p_tenant AND e.run_id=task_run AND e.owner_id=p_task
 AND e.purpose='v30_interchange_import_evidence'
 AND e.artifact_id=photo.v30_uuid5(p_task,'interchange:evidence:1')
 AND e.state='ready' FOR SHARE;
 IF parsed IS NULL THEN RETURN NULL;END IF;
 epoch:=photo.v30_import_person_mask_prepared_live(p_tenant,
  (authority->>'workflow_id')::uuid,p_target_version,
  p_prepared_mask,p_mask_sha,parsed);
 RETURN epoch;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_person_mask_live(uuid,uuid,integer,bigint,uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_person_mask_live(uuid,uuid,integer,bigint,uuid,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_person_mask_live(uuid,uuid,integer,bigint,uuid,uuid,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
