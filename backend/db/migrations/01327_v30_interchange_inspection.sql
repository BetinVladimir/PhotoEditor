-- +goose Up
-- Runtime status follows the actual parent/ingest graph, not report availability.
-- +goose StatementBegin
CREATE FUNCTION photo.inspect_v30_interchange(p_tenant uuid,p_actor uuid,p_session uuid,p_job uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('schema_version',1,'workflow_id',w.id,'tenant_id',w.tenant_id,'project_id',w.project_id,
 'run_id',r.id,'revision',r.revision::text,'status',r.status,
 'stage',CASE WHEN r.status IN('completed','completed_with_errors') THEN 'completed'
 WHEN r.status IN('failed','cancelled','cancelling','queued') THEN r.status
 WHEN r.status='waiting_review' THEN 'review'
 WHEN EXISTS(SELECT 1 FROM photo.collections c WHERE c.tenant_id=w.tenant_id AND c.id=photo.v30_uuid5(w.id,'interchange:draft-collection:1') AND c.deleted_at IS NULL) THEN 'preparing_import'
 WHEN child.id IS NOT NULL THEN 'ingesting' ELSE 'parsing' END,
 'ingest_run_id',child.id,'collection_id',c.id)
 FROM photo.v2_workflows w JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 LEFT JOIN photo.runs child ON child.tenant_id=w.tenant_id AND child.id=photo.v30_uuid5(w.id,'interchange:canonical-ingest:1') AND child.kind='ingest'
 LEFT JOIN photo.collections c ON c.tenant_id=w.tenant_id AND c.id=photo.v30_uuid5(w.id,'interchange:draft-collection:1') AND c.project_id=w.project_id AND c.source_run_id=r.id AND c.deleted_at IS NULL
 WHERE w.tenant_id=p_tenant AND w.id=p_job AND photo.get_v30_interchange(p_tenant,p_actor,p_session,p_job) IS NOT NULL
$$;
ALTER FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.inspect_v30_interchange(uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
