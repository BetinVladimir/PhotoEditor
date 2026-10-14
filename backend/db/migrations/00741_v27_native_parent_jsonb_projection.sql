-- +goose Up
-- Parenthesize JSONB extraction before key subtraction to disambiguate operators.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.semantic_defect_native_parent_ready(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_task uuid,p_anchor jsonb)
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
 IF native ? 'original_producer' AND (jsonb_typeof(native->'original_producer') IS DISTINCT FROM 'object'
 OR ((native->'original_producer')-'producer_task_id'-'params_sha256')<>'{}'
 OR jsonb_typeof(native->'original_producer'->'producer_task_id') IS DISTINCT FROM 'string'
 OR (native->'original_producer'->>'producer_task_id')::uuid IS NULL
 OR coalesce(native->'original_producer'->>'params_sha256','') !~ '^[0-9a-f]{64}$') THEN RETURN false;END IF;
 RETURN child=photo.semantic_uuid_v5((p_anchor->>'parent_run_id')::uuid,'semantic.defect.prepare:reference')
 AND native->'schema_version'='1'::jsonb AND native->>'task_id'=p_task::text
 AND (native-'schema_version'-'params'-'task_id'-'input_digest'-'policy_sha'-'original_producer')=p_anchor;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value THEN RETURN false;
END $$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
