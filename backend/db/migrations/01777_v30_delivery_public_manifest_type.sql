-- A public delivery document is the adopted JSON assembly document, not any
-- ready artifact that happens to occupy the package's manifest_artifact_id.
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.get_v30_delivery(p_tenant uuid,p_actor uuid,p_session uuid,p_package uuid) RETURNS jsonb
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT jsonb_build_object('id',d.id,'revision',d.revision::text,
  'document',CASE WHEN a.id IS NOT NULL THEN jsonb_build_object('artifact_id',a.id,'sha256',a.sha256) ELSE NULL END,
  'data',jsonb_build_object(
   'resource_type','delivery_packages','id',d.id,'tenant_id',d.tenant_id,'project_id',d.project_id,
   'created_at',d.created_at,'revision',d.revision::text,'updated_at',d.updated_at,
   'workflow_id',d.workflow_id,'snapshot_id',d.snapshot_id,'name',d.name,
   'policy_artifact_id',d.policy_artifact_id,'manifest_artifact_id',d.manifest_artifact_id,
   'published_at',d.published_at,'state',d.state))
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authorized
 JOIN photo.delivery_packages d ON d.tenant_id=p_tenant AND d.id=p_package
 JOIN photo.projects p ON p.tenant_id=d.tenant_id AND p.id=d.project_id AND p.archived_at IS NULL
 LEFT JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.manifest_artifact_id AND a.state='ready'
  AND a.media_type='application/json' AND a.output_role='document'
  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND authorized.role IN('owner','admin','editor','viewer')
  AND (authorized.role IN('owner','admin') OR p.visibility='workspace' OR EXISTS(
   SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id
    AND acl.user_id=p_actor AND acl.role IN('editor','viewer')))
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.get_v30_delivery(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.get_v30_delivery(uuid,uuid,uuid,uuid) FROM PUBLIC,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.get_v30_delivery(uuid,uuid,uuid,uuid) TO photo_api;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
