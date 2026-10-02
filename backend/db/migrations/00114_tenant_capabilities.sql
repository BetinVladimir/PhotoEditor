-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.tenant_capabilities()
RETURNS TABLE(operation text,resource_class text,model_id text,supported_backends jsonb,available_backends jsonb)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 WITH tenant_context AS(SELECT nullif(current_setting('app.tenant_id',true),'')::uuid tenant_id),
 available AS(
  SELECT DISTINCT op.operation,backend.value backend
  FROM tenant_context t JOIN photo.worker_registrations w ON w.tenant_id=t.tenant_id AND w.enabled AND w.last_seen>current_timestamp-interval '30 seconds'
  JOIN photo.resource_nodes n ON n.node_id=w.node_id AND n.enabled AND n.reported_at>current_timestamp-interval '30 seconds'
  JOIN photo.operation_registry op ON w.resource_classes ? op.resource_class
  CROSS JOIN LATERAL jsonb_object_keys(coalesce(w.qualified_operations->op.operation,'{}'::jsonb)) backend(value)
  WHERE w.backends ? backend.value AND op.backends ? backend.value
 )
 SELECT op.operation,op.resource_class,op.model_id,op.backends,
  coalesce((SELECT jsonb_agg(a.backend ORDER BY a.backend) FROM available a WHERE a.operation=op.operation),'[]'::jsonb)
 FROM photo.operation_registry op ORDER BY op.operation
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.tenant_capabilities() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.tenant_capabilities() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.tenant_capabilities() TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
