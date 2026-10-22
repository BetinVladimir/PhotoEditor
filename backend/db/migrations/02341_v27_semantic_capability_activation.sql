-- +goose Up
-- Own semantic preview/copy lanes must not advertise readiness while their
-- deployment activation flag is disabled. Preserve legacy operation semantics.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.tenant_capabilities()
RETURNS TABLE(operation text,resource_class text,model_id text,supported_backends jsonb,available_backends jsonb)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 WITH tenant_context AS (
  SELECT nullif(current_setting('app.tenant_id',true),'')::uuid AS tenant_id
 ), available AS (
  SELECT DISTINCT op.operation,backend.value AS backend
  FROM tenant_context tc
  JOIN photo.worker_registrations w ON w.tenant_id=tc.tenant_id
   AND w.enabled AND photo.capacity_policy_worker_fresh(w)
  JOIN photo.resource_nodes n ON n.node_id=w.node_id
   AND n.enabled AND photo.capacity_policy_node_fresh(n)
  JOIN photo.operation_registry op ON w.resource_classes ? op.resource_class
  CROSS JOIN LATERAL (
   SELECT entry.value #>> '{}' AS value
   FROM jsonb_array_elements(CASE WHEN jsonb_typeof(w.qualified_operations->op.operation)='array'
    THEN w.qualified_operations->op.operation ELSE '[]'::jsonb END) entry(value)
   WHERE jsonb_typeof(entry.value)='string'
   UNION
   SELECT entry.key AS value
   FROM jsonb_each(CASE WHEN jsonb_typeof(w.qualified_operations->op.operation)='object'
    THEN w.qualified_operations->op.operation ELSE '{}'::jsonb END) entry(key,value)
  ) backend
  WHERE backend.value<>'' AND w.backends ? backend.value AND op.backends ? backend.value
   AND (op.operation NOT IN ('semantic.preview_local_exposure','semantic.adopt_shown_exposure',
    'semantic.preview_remove_object','semantic.adopt_shown_removal',
    'semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup') OR op.v2_enabled)
 )
 SELECT op.operation,op.resource_class,op.model_id,op.backends,
  coalesce((SELECT jsonb_agg(a.backend ORDER BY a.backend) FROM available a WHERE a.operation=op.operation),'[]'::jsonb)
 FROM photo.operation_registry op ORDER BY op.operation
$$;
ALTER FUNCTION photo.tenant_capabilities() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.tenant_capabilities() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.tenant_capabilities() TO photo_api;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
