-- +goose Up
-- Restoration proposals must remain private and their runs pending until a
-- dedicated physically verified review/Apply path is committed.
-- +goose StatementBegin
ALTER FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 RENAME TO candidate_review_canonical_output_allowed_pre_v28_restoration;
REVOKE ALL ON FUNCTION photo.candidate_review_canonical_output_allowed_pre_v28_restoration(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_api;
CREATE FUNCTION photo.candidate_review_canonical_output_allowed(
 p_actor uuid,p_session uuid,p_tenant uuid,p_task uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.id=p_task
  AND t.operation IN('portrait.glare','image.reflection')) THEN RETURN false;END IF;
 RETURN photo.candidate_review_canonical_output_allowed_pre_v28_restoration(
  p_actor,p_session,p_tenant,p_task);
END $$;
ALTER FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 FROM PUBLIC,photo_scheduler,photo_worker;
GRANT EXECUTE ON FUNCTION photo.candidate_review_canonical_output_allowed(uuid,uuid,uuid,uuid)
 TO photo_api;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid)
 RENAME TO finalize_v2_run_pre_v28_restoration;
REVOKE ALL ON FUNCTION photo.finalize_v2_run_pre_v28_restoration(uuid,uuid)
 FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.finalize_v2_run(p_tenant uuid,p_run uuid)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 THEN RETURN photo.finalize_v2_run_pre_v28_restoration(p_tenant,p_run);END IF;
 IF EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.task_results z
  ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run
   AND t.operation IN('portrait.glare','image.reflection')
   AND t.state='succeeded' AND z.review_required) THEN RETURN 'waiting_review';END IF;
 RETURN photo.finalize_v2_run_pre_v28_restoration(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.finalize_v2_run(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.finalize_v2_run(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.finalize_v2_run(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
