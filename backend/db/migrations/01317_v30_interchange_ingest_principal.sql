-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.v30_ingest_catalog_principal(p_tenant uuid,p_run uuid)
RETURNS TABLE(imported boolean,actor_id uuid,session_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;actor uuid;session uuid;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND kind='ingest';
 IF r.id IS NULL THEN RETURN;END IF;
 IF NOT(r.config_snapshot ? 'interchange_workflow_id') THEN RETURN QUERY SELECT false,NULL::uuid,NULL::uuid;RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=(r.config_snapshot->>'interchange_workflow_id')::uuid AND workflow_type='interchange';
 actor:=r.created_by;
 SELECT (d.v30_interchange_proof->>'auth_session_id')::uuid INTO session FROM photo.backend_documents d
 JOIN photo.tasks t ON t.tenant_id=d.tenant_id AND t.run_id=d.run_id AND t.id=d.owner_id AND t.params_artifact_id=d.artifact_id
 WHERE d.tenant_id=p_tenant AND d.run_id=w.run_id AND d.purpose='task_params' AND d.state='ready' AND t.operation='v2.interchange.parse'
 AND d.v30_interchange_proof->>'project_id'=w.project_id::text;
 IF w.id IS NULL OR session IS NULL OR r.cancel_requested OR photo.v2_editor_authorized(p_tenant,w.project_id,actor,session) IS DISTINCT FROM true
 OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
 OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id AND archived_at IS NULL AND download_allowed)
 OR NOT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=w.run_id AND created_by=actor AND NOT cancel_requested) THEN RETURN;END IF;
 RETURN QUERY SELECT true,actor,session;
END $$;
ALTER FUNCTION photo.v30_ingest_catalog_principal(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_ingest_catalog_principal(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_ingest_catalog_principal(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
