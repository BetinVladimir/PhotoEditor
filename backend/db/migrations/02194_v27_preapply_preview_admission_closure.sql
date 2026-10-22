-- +goose Up
-- Preserve all existing lease/session/source/qualification gates and require
-- independently reconstructed persisted closure before worker pixel IO.
-- +goose StatementBegin
ALTER FUNCTION photo.admit_semantic_exposure_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 RENAME TO admit_semantic_exposure_preview_before_closure;
REVOKE ALL ON FUNCTION photo.admit_semantic_exposure_preview_before_closure(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE FUNCTION photo.admit_semantic_exposure_preview(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_body text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.admit_semantic_exposure_preview_before_closure(p_tenant,p_run,p_task,p_attempt,p_fence,
 p_worker,p_params,p_sha,p_body,p_digest) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN photo.semantic_preapply_preview_closure_ready(p_task) IS TRUE;
END $$;
ALTER FUNCTION photo.admit_semantic_exposure_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_semantic_exposure_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.admit_semantic_exposure_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose StatementEnd
