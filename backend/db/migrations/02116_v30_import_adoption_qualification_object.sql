-- +goose Up
-- Report discovery for a completed canonical import. Reuse the current
-- scheduler policy gates; neither a report locator nor its status alone
-- qualifies the physical report bytes or imported image.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_import_adoption_qualification_object(
 p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,
 p_operation text,p_expected jsonb,p_report uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,
 object_version text,bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE authority jsonb;task_run uuid;parsed text;workflow uuid;
BEGIN
 IF p_operation NOT IN('image.denoise','image.upscale','color.stats','mask.person')
 OR p_report IS NULL OR coalesce(p_sha,'') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_expected) IS DISTINCT FROM 'object'
 OR p_expected->>'Operation' IS DISTINCT FROM p_operation THEN RETURN;END IF;
 authority:=photo.v30_interchange_import_adoption_context(
  p_tenant,p_task,p_attempt,p_fence,p_result);
 IF authority IS NULL THEN RETURN;END IF;
 workflow:=(authority->>'workflow_id')::uuid;
 SELECT t.run_id INTO task_run FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.id=p_task FOR SHARE;
 IF task_run IS NULL THEN RETURN;END IF;
 SELECT e.v30_import_parsed_jcs INTO parsed FROM photo.backend_documents e
 WHERE e.tenant_id=p_tenant AND e.run_id=task_run AND e.owner_id=p_task
 AND e.purpose='v30_interchange_import_evidence'
 AND e.artifact_id=photo.v30_uuid5(p_task,'interchange:evidence:1')
 AND e.state='ready' FOR SHARE;
 IF parsed IS NULL THEN RETURN;END IF;
 IF p_operation='image.denoise' THEN
  RETURN QUERY SELECT * FROM photo.v30_import_denoise_qualification_object(
   p_tenant,workflow,parsed,p_expected,p_report,p_sha);
 ELSIF p_operation='image.upscale' THEN
  RETURN QUERY SELECT * FROM photo.v30_import_upscale_qualification_object(
   p_tenant,workflow,parsed,p_expected,p_report,p_sha);
 ELSIF p_operation='color.stats' THEN
  RETURN QUERY SELECT * FROM photo.v30_import_stats_qualification_object(
   p_tenant,workflow,parsed,p_expected,p_report,p_sha);
 ELSE
  RETURN QUERY SELECT * FROM photo.v30_import_person_mask_qualification_object(
   p_tenant,workflow,parsed,p_expected,p_report,p_sha);
 END IF;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 OR invalid_parameter_value THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_import_adoption_qualification_object(uuid,uuid,integer,bigint,uuid,text,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_import_adoption_qualification_object(uuid,uuid,integer,bigint,uuid,text,jsonb,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v30_import_adoption_qualification_object(uuid,uuid,integer,bigint,uuid,text,jsonb,uuid,text) TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
