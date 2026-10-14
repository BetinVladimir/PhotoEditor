-- +goose Up
-- Exact newly compiled Params document ownership before task creation.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_version_params_object(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_document uuid,p_params uuid,p_params_sha text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.v2_draft_uploads d
 JOIN photo.asset_versions v ON v.tenant_id=d.tenant_id AND v.id=d.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND d.tenant_id=p_tenant AND d.project_id=p_project AND d.document_id=p_document
 AND d.asset_version_id=p_version AND d.upload_contract='Document' AND d.state='ready'
 AND d.artifact_id=p_params AND d.sha256=p_params_sha AND d.bytes=a.bytes AND d.bucket=a.bucket
 AND d.object_version=a.object_version
 AND asset.project_id=p_project AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND v.review_state='accepted' AND v.master_artifact_id=p_source AND v.content_hash=p_source_sha
 AND source.sha256=p_source_sha AND source.state='ready' AND source.verified_at IS NOT NULL
 AND source.output_role='working_image' AND source.media_type='application/json'
 AND source.object_version IS NOT NULL AND source.object_version NOT IN('','null')
 AND (source.expires_at IS NULL OR source.expires_at>clock_timestamp())
 AND a.sha256=p_params_sha AND a.state='ready' AND a.verified_at IS NOT NULL
 AND a.output_role='document' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_defect_version_params_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_version_params_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_version_params_object(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text) TO photo_api;

CREATE FUNCTION photo.lock_semantic_defect_version_params(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_document uuid,p_params uuid,p_params_sha text,p_pin jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE pin record;
BEGIN
 IF jsonb_typeof(p_pin) IS DISTINCT FROM 'object' OR pg_column_size(p_pin)>16384
 OR NOT photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) THEN RETURN false;END IF;
 PERFORM document_id FROM photo.v2_draft_uploads WHERE tenant_id=p_tenant AND document_id=p_document
 AND project_id=p_project AND asset_version_id=p_version AND artifact_id=p_params FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_params FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO pin FROM photo.semantic_defect_version_params_object(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_document,p_params,p_params_sha);
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN p_pin=jsonb_build_object('key',pin.object_key,'version',pin.object_version,'sha256',pin.sha256,'bytes',pin.bytes,'media_type',pin.media_type,'bucket',pin.bucket);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_version_params(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_version_params(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_version_params(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
