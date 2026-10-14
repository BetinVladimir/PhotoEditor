-- +goose Up
-- Exact registered native root and original NPY metadata for a specific parent.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_protection_objects(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_task uuid,p_result uuid,p_anchor jsonb,p_root uuid,p_root_sha text)
RETURNS TABLE(ordinal integer,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE registered photo.semantic_native_protections%ROWTYPE;catalog record;
BEGIN
 IF photo.semantic_defect_native_parent_ready(p_tenant,p_project,p_actor,p_session,p_task,p_anchor) IS NOT TRUE THEN RETURN;END IF;
 SELECT * INTO catalog FROM photo.semantic_defect_native_result_catalog(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_task);
 IF NOT FOUND OR catalog.result_id IS DISTINCT FROM p_result THEN RETURN;END IF;
 SELECT n.* INTO registered FROM photo.semantic_native_protections n WHERE n.tenant_id=p_tenant AND n.project_id=p_project
 AND n.asset_version_id=p_version AND n.source_id=p_source AND n.source_sha=p_source_sha AND n.task_id=p_task AND n.result_id=p_result
 AND n.root_id=p_root AND n.root_sha=p_root_sha AND n.anchor=p_anchor;
 IF NOT FOUND OR jsonb_build_object('artifact_id',registered.pixels_id,'sha256',registered.pixels_sha) IS DISTINCT FROM catalog.outputs->1
 OR jsonb_build_object('artifact_id',registered.report_id,'sha256',registered.report_sha) IS DISTINCT FROM catalog.outputs->4 THEN RETURN;END IF;
 RETURN QUERY
 SELECT selected.ordinal,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM (VALUES(0,registered.root_id,registered.root_sha),(1,registered.pixels_id,registered.pixels_sha)) selected(ordinal,id,sha)
 JOIN photo.artifacts a ON a.tenant_id=p_tenant AND a.id=selected.id AND a.sha256=selected.sha
 WHERE a.state='ready' AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND ((selected.ordinal=0 AND a.output_role='document' AND a.media_type='application/json' AND a.bytes BETWEEN 2 AND 1048576
 AND EXISTS(SELECT 1 FROM photo.v2_draft_uploads u WHERE u.tenant_id=p_tenant AND u.document_id=registered.document_id
 AND u.project_id=p_project AND u.asset_version_id=p_version AND u.artifact_id=a.id AND u.sha256=a.sha256 AND u.state='ready'
 AND u.upload_contract='Document' AND u.bucket=a.bucket AND u.object_version=a.object_version AND u.bytes=a.bytes))
 OR (selected.ordinal=1 AND a.output_role='defect_protection' AND a.media_type='application/octet-stream' AND a.created_by_task_id=p_task))
 ORDER BY selected.ordinal;
END $$;
ALTER FUNCTION photo.semantic_native_protection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_protection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_protection_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
