-- Merge into migration 00196 only after the rest of V21 is wired and tested.
-- qualified_operations[operation] is an array of backend names, not an object.
-- +goose StatementBegin
CREATE FUNCTION photo.tenant_capabilities_evidence()
RETURNS TABLE(operation text,resource_class text,model_id text,
              supported_backends jsonb,available_backends jsonb,evidence jsonb)
LANGUAGE sql SECURITY DEFINER STABLE SET search_path=pg_catalog,photo AS $$
 WITH tenant_context AS (
  SELECT nullif(current_setting('app.tenant_id',true),'')::uuid AS tenant_id
 ), eligible AS (
  SELECT DISTINCT ON (op.operation,backend.value)
   op.operation,backend.value AS backend,n.doctor_report_sha256,w.lock_id,w.certificate_sha256
  FROM tenant_context tc
  JOIN photo.worker_registrations w ON w.tenant_id=tc.tenant_id
   AND w.enabled AND w.last_seen>current_timestamp-interval '30 seconds'
   AND w.certificate_sha256 IS NOT NULL
  JOIN photo.resource_nodes n ON n.node_id=w.node_id
   AND n.enabled AND n.reported_at>current_timestamp-interval '30 seconds'
   AND n.doctor_report_sha256 IS NOT NULL
  JOIN photo.operation_registry op ON w.resource_classes ? op.resource_class
  CROSS JOIN LATERAL jsonb_array_elements_text(
   CASE WHEN jsonb_typeof(w.qualified_operations->op.operation)='array'
    THEN w.qualified_operations->op.operation ELSE '[]'::jsonb END) backend(value)
  WHERE w.backends ? backend.value AND op.backends ? backend.value
   AND (op.model_id IS NULL OR w.lock_id ~ '^[0-9a-f]{64}$')
  ORDER BY op.operation,backend.value,n.doctor_report_sha256,w.worker_id
 )
 SELECT op.operation,op.resource_class,op.model_id,op.backends,
  coalesce((SELECT jsonb_agg(e.backend ORDER BY e.backend)
            FROM eligible e WHERE e.operation=op.operation),'[]'::jsonb),
  coalesce((SELECT jsonb_agg(jsonb_build_object(
             'backend',e.backend,'doctor_report_sha256',e.doctor_report_sha256,
             'model_lock_id',CASE WHEN op.model_id IS NULL THEN NULL ELSE e.lock_id END,
             'worker_certificate_sha256',e.certificate_sha256) ORDER BY e.backend)
            FROM eligible e WHERE e.operation=op.operation),'[]'::jsonb)
 FROM photo.operation_registry op ORDER BY op.operation
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.tenant_capabilities_evidence() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.tenant_capabilities_evidence() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.tenant_capabilities_evidence() TO photo_api;
