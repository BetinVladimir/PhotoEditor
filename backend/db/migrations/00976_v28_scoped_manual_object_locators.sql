-- +goose Up
-- Scoped immutable locators; scheduler receives no direct artifact table privileges.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_manual_pair_object(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_target uuid,p_target_sha text,p_guard uuid,p_guard_sha text,p_target_pixel uuid,p_target_pixel_sha text,p_guard_pixel uuid,p_guard_pixel_sha text,p_artifact uuid,p_sha text)
RETURNS TABLE(bytes bigint,bucket text,object_key text,object_version text,media_type text,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
SELECT a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,a.sha256 FROM photo.v28_manual_pair_origin(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,p_target,p_target_sha,p_guard,p_guard_sha,p_target_pixel,p_target_pixel_sha,p_guard_pixel,p_guard_pixel_sha) o JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=p_artifact AND a.sha256=p_sha AND ((a.id=o.root_id AND a.sha256=o.root_sha) OR (a.id=o.params_id AND a.sha256=o.params_sha)) WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_manual_pair_object(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_manual_pair_object(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_manual_pair_object(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v28_manual_vectors_object(p_tenant uuid,p_project uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,p_target uuid,p_target_sha text,p_guard uuid,p_guard_sha text,p_target_pixel uuid,p_target_pixel_sha text,p_guard_pixel uuid,p_guard_pixel_sha text,p_root uuid,p_root_sha text,p_params uuid,p_params_sha text,p_vectors uuid,p_vectors_sha text)
RETURNS TABLE(bytes bigint,bucket text,object_key text,object_version text,media_type text,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
SELECT a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,a.sha256 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=p_vectors AND a.sha256=p_vectors_sha AND photo.v28_manual_vectors_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,p_target,p_target_sha,p_guard,p_guard_sha,p_target_pixel,p_target_pixel_sha,p_guard_pixel,p_guard_pixel_sha,p_root,p_root_sha,p_params,p_params_sha,p_vectors,p_vectors_sha) IS TRUE AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version NOT IN ('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_manual_vectors_object(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_manual_vectors_object(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_manual_vectors_object(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
