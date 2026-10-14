-- +goose Up
-- Viewer-scoped delivery metadata. The existing delivery_packages_project_page
-- index supplies the tenant/project keyset order; no object address is returned.
-- +goose StatementBegin
CREATE FUNCTION photo.list_v30_delivery_packages(
 p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,
 p_after_at timestamptz,p_after_id uuid,p_limit integer)
RETURNS TABLE(id uuid,project_id uuid,workflow_id uuid,name text,state text,
 revision bigint,created_at timestamptz)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS NULL OR p_actor IS NULL OR p_session IS NULL OR p_project IS NULL
    OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 101
    OR (p_after_at IS NULL)<>(p_after_id IS NULL)
    OR p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN
  RAISE EXCEPTION 'INVALID_DELIVERY_LIST_SCOPE' USING ERRCODE='22023';
 END IF;
 RETURN QUERY
 SELECT d.id,d.project_id,d.workflow_id,d.name,d.state,d.revision,d.created_at
 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) authorized
 JOIN photo.projects p ON p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL
 JOIN photo.delivery_packages d ON d.tenant_id=p.tenant_id AND d.project_id=p.id
 WHERE authorized.role IN('owner','admin','editor','viewer')
  AND (authorized.role IN('owner','admin') OR p.visibility='workspace' OR EXISTS(
   SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=p.id
    AND acl.user_id=p_actor AND acl.role IN('editor','viewer')))
  AND (p_after_id IS NULL OR (d.created_at,d.id)>(p_after_at,p_after_id))
 ORDER BY d.created_at,d.id LIMIT p_limit;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.list_v30_delivery_packages(uuid,uuid,uuid,uuid,timestamptz,uuid,integer) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.list_v30_delivery_packages(uuid,uuid,uuid,uuid,timestamptz,uuid,integer) FROM PUBLIC,photo_worker,photo_scheduler;
GRANT EXECUTE ON FUNCTION photo.list_v30_delivery_packages(uuid,uuid,uuid,uuid,timestamptz,uuid,integer) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
