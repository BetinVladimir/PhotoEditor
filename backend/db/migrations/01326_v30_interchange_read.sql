-- +goose Up
-- Read resource projection for the currently installed bundle import path.
-- No object locations, auth sessions or foreign project metadata are returned.
-- +goose StatementBegin
CREATE FUNCTION photo.get_v30_interchange(p_tenant uuid,p_actor uuid,p_session uuid,p_job uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('id',w.id,'revision',r.revision::text,'document',NULL,
 'data',jsonb_build_object('resource_type','interchange_jobs','id',w.id,'tenant_id',w.tenant_id,'project_id',w.project_id,
 'created_at',r.created_at,'updated_at',coalesce((SELECT max(e.created_at) FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id),r.created_at),
 'revision',r.revision::text,'workflow_id',w.id,'direction','import','format','photoeditor_bundle',
 'mapping_artifact_id',NULL,'report_artifact_id',NULL,'bundle_artifact_id',NULL))
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authorized
 JOIN photo.v2_workflows w ON w.tenant_id=p_tenant AND w.id=p_job AND w.workflow_type='interchange'
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id AND r.kind='v2' AND r.graph_sealed AND r.status<>'draft'
 JOIN photo.projects p ON p.tenant_id=w.tenant_id AND p.id=w.project_id AND p.archived_at IS NULL
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid AND authorized.role IN('owner','admin','editor','viewer')
 AND (authorized.role IN('owner','admin') OR p.visibility='workspace' OR EXISTS(SELECT 1 FROM photo.project_acl acl
 WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id AND acl.user_id=p_actor AND acl.role IN('editor','viewer')))
 AND EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.owner_id=t.id AND d.artifact_id=t.params_artifact_id AND d.purpose='task_params' AND d.state='ready'
 WHERE t.tenant_id=w.tenant_id AND t.run_id=w.run_id AND t.operation='v2.interchange.parse' AND t.operation_version='1'
 AND d.v30_interchange_proof->>'project_id'=w.project_id::text)
$$;
ALTER FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.get_v30_interchange(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
