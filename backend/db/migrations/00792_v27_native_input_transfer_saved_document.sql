-- +goose Up
-- Read-only original snapshot/Params object pins; no reservation or repair.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_transfer_saved_document(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_ordinal integer,p_root uuid,p_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.runs r
 JOIN photo.v2_draft_uploads u ON u.tenant_id=r.tenant_id AND u.project_id=p_project
 AND u.document_id=photo.semantic_uuid_v5(p_run,CASE WHEN p_ordinal=-1 THEN 'semantic.input_snapshot' ELSE 'semantic.transfer.params:'||p_ordinal::text END)
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND r.tenant_id=p_tenant AND r.id=p_run AND r.kind='v2' AND r.created_by=p_actor
 AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_snapshot->'v27_semantic'->>'auth_session_id'=p_session::text
 AND p_ordinal BETWEEN -1 AND 4999
 AND CASE WHEN p_ordinal=-1 THEN r.config_snapshot->'v27_semantic'->'snapshot'=jsonb_build_object('artifact_id',p_root,'sha256',p_sha)
 ELSE r.config_snapshot->'v27_semantic'->'nodes'->p_ordinal->>'params_artifact_id'=p_root::text AND r.config_snapshot->'v27_semantic'->'nodes'->p_ordinal->>'params_sha'=p_sha END
 AND u.state='ready' AND u.artifact_id=p_root AND u.sha256=p_sha
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_version=u.object_version
 AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND 8388608 AND a.media_type='application/json'
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_native_input_transfer_saved_document(uuid,uuid,uuid,uuid,uuid,integer,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_transfer_saved_document(uuid,uuid,uuid,uuid,uuid,integer,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_transfer_saved_document(uuid,uuid,uuid,uuid,uuid,integer,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
