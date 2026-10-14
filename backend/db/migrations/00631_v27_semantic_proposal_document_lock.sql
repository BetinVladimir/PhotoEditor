-- +goose Up
-- Publication identity is run-derived; caller separately locks full inventory.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_proposal_document(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_version uuid,p_artifact uuid,p_sha text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE found_id uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_run IS NULL OR p_run='00000000-0000-0000-0000-000000000000'::uuid
 OR p_sha IS NULL OR p_sha !~ '^[0-9a-f]{64}$' OR p_bytes IS NULL OR p_bytes NOT BETWEEN 2 AND 1048576
 OR NOT photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) THEN RETURN false;END IF;
 SELECT a.id INTO found_id FROM photo.v2_draft_uploads u
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 WHERE u.tenant_id=p_tenant AND u.project_id=p_project AND u.asset_version_id=p_version
 AND u.document_id=photo.semantic_uuid_v5(p_run,'semantic.edit_intent')
 AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.bytes=p_bytes
 AND u.upload_contract='Document' AND u.state='ready' AND u.completed_at IS NOT NULL
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.output_role='document'
 AND a.media_type='application/json' AND a.sha256=u.sha256 AND a.bytes=u.bytes
 AND a.bucket=u.bucket AND a.object_version=u.object_version
 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND a.object_key='tenants/'||p_tenant||'/v2-drafts/'||p_artifact
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 FOR SHARE OF u,a;
 RETURN found_id IS NOT NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.lock_semantic_proposal_document(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_proposal_document(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_proposal_document(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
