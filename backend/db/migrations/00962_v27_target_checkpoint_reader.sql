-- +goose Up
-- Authenticated routing metadata only; locators are not admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_target_checkpoint(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_request_sha text,p_snapshot uuid)
RETURNS TABLE(manifest_text text,manifest_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT r.v27_target_manifest,r.v27_target_manifest_sha FROM photo.runs r
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND r.tenant_id=p_tenant AND r.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference')
 AND r.kind='v2' AND r.graph_sealed AND NOT r.cancel_requested AND r.created_by=p_actor
 AND r.input_snapshot_id=p_snapshot AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_snapshot->'v27_detector_prepare'->>'auth_session_id'=p_session::text
 AND r.config_snapshot->'v27_detector_prepare'->>'parent_run_id'=p_parent::text
 AND r.config_snapshot->'v27_detector_prepare'->>'request_sha'=p_request_sha
 AND r.v27_initial_manual_selection IS NULL
 AND r.v27_target_manifest IS NOT NULL AND octet_length(r.v27_target_manifest) BETWEEN 2 AND 8388608
 AND r.v27_target_manifest_sha=encode(public.digest(convert_to(r.v27_target_manifest,'UTF8'),'sha256'),'hex');
$$;
ALTER FUNCTION photo.semantic_native_target_checkpoint(uuid,uuid,uuid,uuid,uuid,text,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_target_checkpoint(uuid,uuid,uuid,uuid,uuid,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_target_checkpoint(uuid,uuid,uuid,uuid,uuid,text,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
