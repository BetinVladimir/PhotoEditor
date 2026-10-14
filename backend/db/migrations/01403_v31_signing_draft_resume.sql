-- +goose Up
-- Existing exact drafts (including blocked materialized task) resume without
-- invoking the empty-draft constructor. Current source/session scope repeats.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_signing_draft_present(p_tenant uuid,p_actor uuid,p_session uuid,p_key text,p_request_sha text,p_task uuid,p_artifact uuid,p_sha text,p_bytes bigint,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 RETURN photo.v31_signing_params_scope(p_tenant,p_actor,p_session,p_key,p_request_sha,p_task,p_artifact,p_sha,p_bytes,p_bucket) IS NOT NULL;
END $$;
ALTER FUNCTION photo.v31_signing_draft_present(uuid,uuid,uuid,text,text,uuid,uuid,text,bigint,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_signing_draft_present(uuid,uuid,uuid,text,text,uuid,uuid,text,bigint,text) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v31_signing_draft_present(uuid,uuid,uuid,text,text,uuid,uuid,text,bigint,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
