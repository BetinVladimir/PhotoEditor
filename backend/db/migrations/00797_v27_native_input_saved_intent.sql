-- +goose Up
-- Original intent object locator anchored to its saved native detector graph.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_saved_intent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_version uuid)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.runs r JOIN photo.v2_draft_uploads u ON u.tenant_id=r.tenant_id AND u.project_id=p_project AND u.asset_version_id=p_version
 AND u.document_id=photo.semantic_uuid_v5(p_parent,'semantic.edit_intent')
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND r.tenant_id=p_tenant AND r.id=photo.semantic_uuid_v5(p_parent,'semantic.face.detect:reference') AND r.kind='v2' AND r.created_by=p_actor
 AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_snapshot->'v27_detector_prepare'->>'parent_run_id'=p_parent::text
 AND r.config_snapshot->'v27_detector_prepare'->>'auth_session_id'=p_session::text
 AND r.config_snapshot->'v27_detector_prepare'->'source'->>'asset_version_id'=p_version::text
 AND r.config_snapshot->'v27_detector_prepare'->'intent'=jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)
 AND u.upload_contract='Document' AND u.state='ready' AND u.completed_at IS NOT NULL
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document' AND a.media_type='application/json'
 AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_version=u.object_version
 AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_key='tenants/'||p_tenant||'/v2-drafts/'||a.id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_native_input_saved_intent(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_saved_intent(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_saved_intent(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
