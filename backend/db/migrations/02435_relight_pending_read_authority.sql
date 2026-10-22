-- +goose Up
-- Pending media uses the same live authority as selected media, without requiring a decision.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_relight_review_read_ready(p_tenant uuid,p_review uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v28_relight_review_authority(p_tenant,p_review,p_actor,p_session)
$$;
ALTER FUNCTION photo.v28_relight_review_read_ready(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_relight_review_read_ready(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_relight_review_read_ready(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
