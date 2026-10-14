-- +goose Up
-- Resume an exact already prepared draft; never replace stale existing state.
-- +goose StatementBegin
CREATE FUNCTION photo.ensure_v29_replacement_detector_draft(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_descriptor text,p_original text,p_snapshot uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.read_v29_replacement_detector_draft(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_descriptor,p_original,p_snapshot) IS NOT NULL THEN RETURN true;END IF;
 RETURN photo.save_v29_replacement_detector_draft(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_descriptor,p_original,p_snapshot);
END $$;
ALTER FUNCTION photo.ensure_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.ensure_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.ensure_v29_replacement_detector_draft(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
