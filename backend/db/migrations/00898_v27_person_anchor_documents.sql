-- +goose Up
-- Original preparation anchor documents; read-only locators, no admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_person_saved_anchor_document(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_config_sha text,p_kind text)
RETURNS TABLE(artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.semantic_native_input_saved_stage(p_tenant,p_project,p_actor,p_session,p_parent,photo.semantic_uuid_v5(p_parent,'semantic.mask.person:reference'),'mask.person') original
 JOIN photo.v2_draft_uploads u ON u.tenant_id=p_tenant AND u.project_id=p_project
 AND u.document_id=photo.semantic_uuid_v5(p_parent,CASE p_kind WHEN 'intent' THEN 'semantic.edit_intent' WHEN 'snapshot' THEN 'semantic.input_snapshot' END)
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE p_kind IN('intent','snapshot') AND p_config_sha ~ '^[0-9a-f]{64}$' AND original.config_hash=p_config_sha
 AND original.config_snapshot->'v27_person_prepare'->p_kind=jsonb_build_object('artifact_id',a.id,'sha256',a.sha256)
 AND (p_kind<>'intent' OR u.asset_version_id::text=original.config_snapshot->'v27_person_prepare'->'source'->>'asset_version_id')
 AND u.upload_contract='Document' AND u.state='ready' AND u.completed_at IS NOT NULL
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document' AND a.media_type='application/json'
 AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_version=u.object_version
 AND a.object_version NOT IN('','null') AND a.bytes BETWEEN 2 AND CASE WHEN p_kind='intent' THEN 1048576 ELSE 8388608 END
 AND a.object_key='tenants/'||p_tenant||'/v2-drafts/'||a.id
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
ALTER FUNCTION photo.semantic_person_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_person_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_person_saved_anchor_document(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
