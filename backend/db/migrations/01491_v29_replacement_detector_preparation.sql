-- +goose Up
-- Lock-only scheduler preparation; never grants publication or source object IO.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_replacement_detector_preparation(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_descriptor text,p_original text,p_snapshot uuid)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE d jsonb;
BEGIN
 IF photo.lock_v29_replacement_detector_identity(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan,p_descriptor,p_original) IS DISTINCT FROM true THEN RETURN false;END IF;
 d:=p_descriptor::jsonb;
 RETURN photo.lock_v29_negative_replacement_snapshot(p_tenant,p_project,p_actor,p_session,p_review,p_hash,
 (d->'replacement'->>'original_task_id')::uuid,p_snapshot) IS TRUE;
END $$;
ALTER FUNCTION photo.lock_v29_replacement_detector_preparation(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_replacement_detector_preparation(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) FROM PUBLIC,photo_api,photo_worker,photo_portal,photo_gateway;
GRANT EXECUTE ON FUNCTION photo.lock_v29_replacement_detector_preparation(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,text,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
