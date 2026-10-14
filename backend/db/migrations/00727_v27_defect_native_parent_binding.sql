-- +goose Up
-- Explicit parent-request association, separate from qualification/admission.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_native_parent_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_anchor jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native jsonb; child uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE
 OR jsonb_typeof(p_anchor) IS DISTINCT FROM 'object' OR octet_length(p_anchor::text)>8192
 OR (p_anchor-'parent_run_id'-'request_sha'-'auth_session_id'-'source'-'intent'-'snapshot')<>'{}'
 OR p_anchor->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR coalesce(p_anchor->>'request_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT r.id,r.config_snapshot->'v27_defect_prepare' INTO child,native
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.operation='defects.propose' AND t.operation_version='1'
 AND t.state='succeeded' AND NOT t.cancel_requested AND r.graph_sealed AND NOT r.cancel_requested
 AND r.status IN('running','waiting_review','completed') AND w.project_id=p_project AND w.workflow_type='semantic' AND w.renderer_version='v2.2';
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN child=photo.semantic_uuid_v5((p_anchor->>'parent_run_id')::uuid,'semantic.defect.prepare:reference')
 AND native->'schema_version'='1'::jsonb AND native->>'task_id'=p_task::text
 AND (native-'schema_version'-'params'-'task_id'-'input_digest'-'policy_sha')=p_anchor;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_native_parent_ready(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_native_parent_ready(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_native_parent_ready(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_api;
CREATE FUNCTION photo.lock_semantic_defect_native_parent(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_anchor jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_defect_native_parent_ready(p_tenant,p_project,p_actor,p_session,p_task,p_anchor) IS NOT TRUE THEN RETURN false;END IF;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR SHARE;
 PERFORM r.id FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.tenant_id=p_tenant AND t.id=p_task FOR SHARE OF r;
 RETURN photo.semantic_defect_native_parent_ready(p_tenant,p_project,p_actor,p_session,p_task,p_anchor);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_native_parent(uuid,uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_native_parent(uuid,uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_native_parent(uuid,uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
