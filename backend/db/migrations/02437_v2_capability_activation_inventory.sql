-- +goose Up
-- Every declared V20-V32 operation requires explicit activation before it is
-- advertised as available. Preserve legacy operations and existing qualification,
-- model lock, tenant scope and heartbeat gates.
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
   AND (op.model_id IS NULL OR w.lock_id ~ '^[0-9a-f]{64}$')
   AND (op.operation NOT IN ('layer.compose','session.describe','session.plan','learning.fit','learning.evaluate','capture.assess','identity.compare','semantic.transfer','portrait.open_eyes','portrait.glare','image.reflection','portrait.hair','portrait.clothing','image.relight','optical.lens','optical.perspective','optical.negative','optical.panorama','optical.focus_stack','provenance.sign','v2.delivery.assemble','v2.interchange.import','v2.interchange.export','v2.semantic.assemble','v2.interchange.parse','v2.delivery.prepare','region.preview','render.compose','semantic.local_exposure','optical.perspective_lines','semantic.adopt_shown_exposure','semantic.preview_remove_object','semantic.adopt_shown_removal','semantic.preview_background_cleanup','semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region','semantic.preview_retouch_region') OR op.v2_enabled)
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
