-- +goose Up
-- Original registered protection locator only; existing object/result reader
-- revalidates ready provenance. No publication or registration permission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_native_input_saved_protection(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_parent uuid,p_task uuid)
RETURNS TABLE(root_id uuid,root_sha text,document_id uuid,result_id uuid)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT n.root_id,n.root_sha,n.document_id,n.result_id
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.semantic_native_protections n ON n.tenant_id=t.tenant_id AND n.task_id=t.id AND n.project_id=p_project AND n.parent_run_id=p_parent
 JOIN LATERAL photo.semantic_native_input_protection_objects(p_tenant,p_project,p_actor,p_session,n.asset_version_id,n.source_id,n.source_sha,t.id,n.result_id,n.anchor,n.root_id,n.root_sha) object ON object.ordinal=0
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session)
 AND t.tenant_id=p_tenant AND t.id=p_task AND t.operation='defects.propose' AND t.operation_version='1'
 AND r.kind='v2' AND r.created_by=p_actor AND r.config_snapshot->>'project_id'=p_project::text
 AND r.config_snapshot->'v27_defect_native_prepare'->>'parent_run_id'=p_parent::text
 AND r.config_snapshot->'v27_defect_native_prepare'->>'auth_session_id'=p_session::text
 AND n.document_id=photo.semantic_uuid_v5(p_parent,'semantic.defect.protection:'||p_task::text)
 AND n.anchor->>'auth_session_id'=p_session::text;
$$;
ALTER FUNCTION photo.semantic_native_input_saved_protection(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_native_input_saved_protection(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_native_input_saved_protection(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
