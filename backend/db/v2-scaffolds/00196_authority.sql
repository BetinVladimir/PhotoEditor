-- Revalidate authenticated session and project edit rights inside every v2 mutation.
-- +goose StatementBegin
CREATE FUNCTION photo.v2_editor_authorized(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT EXISTS(
  SELECT 1 FROM photo.authorize_tenant(p_actor,p_session,p_tenant) auth
  JOIN photo.projects p ON p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL
  WHERE auth.role IN('owner','admin') OR
   (auth.role='editor' AND (p.visibility='workspace' OR EXISTS(
    SELECT 1 FROM photo.project_acl acl WHERE acl.tenant_id=p_tenant AND acl.project_id=p_project
     AND acl.user_id=p_actor AND acl.role='editor')))
 )
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_editor_authorized(uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_editor_authorized(uuid,uuid,uuid,uuid) FROM PUBLIC;

-- Workflow status verifies canonical output roots against the immutable task result.
-- The existing tenant RLS policy limits these read-only rows to app.tenant_id.
GRANT SELECT ON photo.task_results TO photo_api;
