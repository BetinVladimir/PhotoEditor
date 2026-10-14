-- +goose Up
-- Renewal keeps one exact canonical result fenced during bounded archive IO.
-- It cannot recover an expired or replaced token and grants no object access.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_renew_export_adoption_claim(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE renewed boolean;
BEGIN
 UPDATE photo.v2_workflows w
 SET v30_export_adoption_retry_after=clock_timestamp()+interval '31 minutes'
 WHERE w.tenant_id=p_tenant AND w.id=p_workflow
  AND w.v30_export_adoption_token=p_token
  AND w.v30_export_adoption_retry_after>clock_timestamp()
  AND photo.v30_export_adoption_claim_current(
   p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token)
 RETURNING true INTO renewed;
 RETURN coalesce(renewed,false);
END $$;
ALTER FUNCTION photo.v30_renew_export_adoption_claim(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_renew_export_adoption_claim(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_renew_export_adoption_claim(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
